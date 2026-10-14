package api

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"hearth/internal/files"
	"hearth/internal/perms"
	"hearth/internal/servers"
	"hearth/internal/settings"
	"hearth/internal/store"
)

func cleanParam(p string) (string, error) {
	c, err := files.Clean(p)
	if err != nil {
		return "", badRequest("Ungültiger Pfad")
	}
	return c, nil
}

func fsErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrNotExist):
		return errStatus(http.StatusNotFound, "Datei oder Ordner nicht gefunden")
	case errors.Is(err, os.ErrPermission):
		return errStatus(http.StatusForbidden, "Zugriff verweigert")
	case errors.Is(err, files.ErrExists):
		return errStatus(http.StatusConflict, err.Error())
	case strings.Contains(err.Error(), "path escapes"):
		return badRequest("Pfad liegt außerhalb des Serverordners")
	}
	return err
}

// GET /api/servers/{id}/files?path=
func (a *API) listFiles(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	p, err := cleanParam(r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	list, err := files.New(inst.Dir()).List(p)
	if err != nil {
		return fsErr(err)
	}
	ok(w, map[string]any{"path": p, "entries": list})
	return nil
}

// GET /api/servers/{id}/files/content?path=
func (a *API) readFile(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	p, err := cleanParam(r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	fs := files.New(inst.Dir())
	b, err := fs.ReadText(p, 8<<20)
	if err != nil {
		return fsErr(err)
	}
	st, _ := fs.Stat(p)
	var mod int64
	if st != nil {
		mod = st.ModTime().Unix()
	}
	ok(w, map[string]any{"path": p, "content": string(b), "size": len(b), "modTime": mod})
	return nil
}

// PUT /api/servers/{id}/files/content?path=
func (a *API) writeFile(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	p, err := cleanParam(r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	var req struct {
		Content string `json:"content"`
		// ExpectModTime protects against overwriting concurrent changes (0 = skip check).
		ExpectModTime int64 `json:"expectModTime"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	fs := files.New(inst.Dir())
	if req.ExpectModTime > 0 {
		if st, err := fs.Stat(p); err == nil && st.ModTime().Unix() > req.ExpectModTime {
			return errStatus(http.StatusConflict, "Die Datei wurde inzwischen geändert. Bitte neu laden.")
		}
	}
	if err := fs.WriteFile(p, []byte(req.Content)); err != nil {
		return fsErr(err)
	}
	st, _ := fs.Stat(p)
	a.audit(r, inst.ID(), "files.write", map[string]any{"path": p})
	out := map[string]any{"path": p}
	if st != nil {
		out["modTime"] = st.ModTime().Unix()
	}
	ok(w, out)
	return nil
}

// GET /api/servers/{id}/files/download?path= (file or folder as zip) or ?paths=a&paths=b
func (a *API) downloadFiles(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	q := r.URL.Query()
	var paths []string
	for _, raw := range append(q["paths"], q["path"]...) {
		if raw == "" {
			continue
		}
		p, err := cleanParam(raw)
		if err != nil {
			return err
		}
		paths = append(paths, p)
	}
	fs := files.New(inst.Dir())
	if len(paths) == 1 {
		st, err := fs.Stat(paths[0])
		if err != nil {
			return fsErr(err)
		}
		if !st.IsDir() {
			f, st, err := fs.OpenRead(paths[0])
			if err != nil {
				return fsErr(err)
			}
			defer f.Close()
			name := path.Base(paths[0])
			ctype := mime.TypeByExtension(path.Ext(name))
			if ctype == "" || strings.HasPrefix(ctype, "text/html") {
				ctype = "application/octet-stream"
			}
			w.Header().Set("Content-Type", ctype)
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
			http.ServeContent(w, r, name, st.ModTime(), f)
			return nil
		}
	}
	name := "dateien"
	if len(paths) == 1 {
		name = path.Base(paths[0])
	} else if len(paths) == 0 {
		name = strings.ReplaceAll(strings.ToLower(inst.Server().Name), " ", "-")
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".zip"}))
	if _, _, err := fs.ZipPaths(w, paths, nil, nil); err != nil {
		a.Log.Warn("zip download", "err", err)
	}
	return nil
}

// POST /api/servers/{id}/files/upload?path=dir (multipart, field "files"; relative paths via filename)
func (a *API) uploadFiles(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	dir, err := cleanParam(r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	limit := int64(a.Settings.Int(settings.UploadLimitMB)) << 20
	srv := inst.Server()
	if srv.DiskLimitMB > 0 {
		free := int64(srv.DiskLimitMB)<<20 - inst.DiskBytes()
		if free <= 0 {
			return badRequest("Das Speicherlimit des Servers ist erreicht")
		}
		if limit == 0 || free < limit {
			limit = free
		}
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return badRequest("Ungültiger Upload")
	}
	fs := files.New(inst.Dir())
	var total int64
	var names []string
	// Folder uploads: the client sends a "relpath" field before each file part,
	// because multipart file names are reduced to their base name.
	nextRel := ""
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return badRequest("Upload abgebrochen: " + err.Error())
		}
		if part.FormName() == "relpath" {
			b, _ := io.ReadAll(io.LimitReader(part, 4096))
			nextRel = string(b)
			part.Close()
			continue
		}
		if part.FormName() != "files" {
			part.Close()
			continue
		}
		name := part.FileName()
		if nextRel != "" {
			name = nextRel
			nextRel = ""
		}
		if name == "" {
			part.Close()
			continue
		}
		rel, err := files.Clean(path.Join(dir, name))
		if err != nil || rel == "" || !strings.HasPrefix(rel+"/", dir+"/") && dir != "" {
			part.Close()
			return badRequest("Ungültiger Dateiname: " + name)
		}
		f, err := fs.Create(rel + ".hearth-upload")
		if err != nil {
			part.Close()
			return fsErr(err)
		}
		var src io.Reader = part
		if limit > 0 {
			src = io.LimitReader(part, limit-total+1)
		}
		n, err := io.Copy(f, src)
		f.Close()
		part.Close()
		total += n
		if err != nil {
			fs.Remove(rel + ".hearth-upload")
			return badRequest("Upload fehlgeschlagen: " + err.Error())
		}
		if limit > 0 && total > limit {
			fs.Remove(rel + ".hearth-upload")
			return badRequest(fmt.Sprintf("Upload überschreitet das Limit (%s)", fmtBytes(limit)))
		}
		fs.Remove(rel)
		if err := fs.Rename(rel+".hearth-upload", rel); err != nil {
			return fsErr(err)
		}
		names = append(names, rel)
	}
	inst.RefreshDisk()
	a.audit(r, inst.ID(), "files.upload", map[string]any{"files": names, "bytes": total})
	ok(w, map[string]any{"uploaded": names, "bytes": total})
	return nil
}

type pathsReq struct {
	Path  string   `json:"path"`
	Paths []string `json:"paths"`
	From  string   `json:"from"`
	To    string   `json:"to"`
	Dest  string   `json:"dest"`
}

// POST /api/servers/{id}/files/{op}
func (a *API) fileOp(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	op := r.PathValue("op")
	var req pathsReq
	if err := readJSON(r, &req); err != nil {
		return err
	}
	fs := files.New(inst.Dir())
	need := perms.FilesWrite
	if op == "delete" {
		need = perms.FilesDelete
	}
	if !ps.Has(need) {
		return errForbidden
	}
	var err error
	details := map[string]any{}
	switch op {
	case "mkdir":
		p, cerr := cleanParam(req.Path)
		if cerr != nil {
			return cerr
		}
		err = fs.Mkdir(p)
		details["path"] = p
	case "touch":
		p, cerr := cleanParam(req.Path)
		if cerr != nil {
			return cerr
		}
		if fs.Exists(p) {
			return errStatus(http.StatusConflict, "Datei existiert bereits")
		}
		err = fs.WriteFile(p, nil)
		details["path"] = p
	case "rename", "copy":
		from, cerr := cleanParam(req.From)
		if cerr != nil {
			return cerr
		}
		to, cerr := cleanParam(req.To)
		if cerr != nil {
			return cerr
		}
		if op == "rename" {
			err = fs.Rename(from, to)
		} else {
			err = fs.Copy(from, to)
		}
		details["from"], details["to"] = from, to
	case "delete":
		var done []string
		for _, raw := range req.Paths {
			p, cerr := cleanParam(raw)
			if cerr != nil || p == "" {
				continue
			}
			if e := fs.Remove(p); e != nil {
				err = e
				break
			}
			done = append(done, p)
		}
		details["paths"] = done
	case "compress":
		var paths []string
		for _, raw := range req.Paths {
			p, cerr := cleanParam(raw)
			if cerr != nil || p == "" {
				return badRequest("Ungültiger Pfad")
			}
			paths = append(paths, p)
		}
		if len(paths) == 0 {
			return badRequest("Keine Dateien ausgewählt")
		}
		dest, cerr := cleanParam(req.Dest)
		if cerr != nil || dest == "" {
			dest = path.Join(path.Dir(paths[0]), "archiv-"+time.Now().Format("2006-01-02_15-04")+".zip")
			dest = strings.TrimPrefix(dest, "./")
		}
		if !strings.HasSuffix(strings.ToLower(dest), ".zip") {
			dest += ".zip"
		}
		err = fs.Compress(paths, dest)
		details["paths"], details["dest"] = paths, dest
	case "extract":
		p, cerr := cleanParam(req.Path)
		if cerr != nil {
			return cerr
		}
		dest, cerr := cleanParam(req.Dest)
		if cerr != nil {
			return cerr
		}
		var max int64
		if lim := inst.Server().DiskLimitMB; lim > 0 {
			max = int64(lim)<<20 - inst.DiskBytes()
			if max <= 0 {
				return badRequest("Das Speicherlimit des Servers ist erreicht")
			}
		}
		err = fs.Extract(p, dest, max)
		details["path"], details["dest"] = p, dest
	default:
		return errNotFound
	}
	if err != nil {
		return fsErr(err)
	}
	inst.RefreshDisk()
	a.audit(r, inst.ID(), "files."+op, details)
	ok(w, nil)
	return nil
}
