package tools

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"notcursor.ai/app/internal/gitx"
	"notcursor.ai/app/internal/projindex"
	"notcursor.ai/app/internal/projmem"
	"notcursor.ai/app/internal/workspace"
)

// Рабочее пространство из нескольких папок. Файловые инструменты видят папки как
// верхнеуровневые каталоги («api/main.go»), а всё, что привязано к репозиторию
// или модулю — git, индекс символов, go vet, память — работает по каждой папке
// отдельно. Здесь собраны эти «поштучные» операции.

// folders — папки рабочего пространства прогона (одна для обычного проекта).
func (r *Registry) folders() ([]workspace.Folder, error) {
	ws := r.ws()
	if ws == nil {
		return nil, fmt.Errorf("no active workspace")
	}
	fs := ws.Folders()
	if len(fs) == 0 {
		return nil, fmt.Errorf("no active project")
	}
	return fs, nil
}

// folderHint — какой папке принадлежит путь «папка/файл». Пусто — путь без
// префикса папки (или рабочее пространство из одной папки).
func folderHint(folders []workspace.Folder, p string) string {
	if len(folders) < 2 {
		return ""
	}
	p = strings.Trim(strings.ReplaceAll(strings.TrimSpace(p), "\\", "/"), "/")
	if p == "" || p == "." {
		return ""
	}
	first := p
	if i := strings.Index(p, "/"); i >= 0 {
		first = p[:i]
	}
	for _, f := range folders {
		if strings.EqualFold(f.Name, first) {
			return f.Name
		}
	}
	return ""
}

// stripFolder убирает префикс папки: репозиторий ждёт путь относительно себя.
func stripFolder(f workspace.Folder, multi bool, p string) string {
	if !multi {
		return p
	}
	norm := strings.Trim(strings.ReplaceAll(strings.TrimSpace(p), "\\", "/"), "/")
	if strings.EqualFold(norm, f.Name) {
		return ""
	}
	prefix := strings.ToLower(f.Name) + "/"
	if strings.HasPrefix(strings.ToLower(norm), prefix) {
		return norm[len(prefix):]
	}
	return p
}

type gitTarget struct {
	svc    *gitx.Service
	folder workspace.Folder
	multi  bool
}

func (t gitTarget) rel(p string) string { return stripFolder(t.folder, t.multi, p) }

func (t gitTarget) rels(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if s := t.rel(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// gitTargets выбирает репозитории для git-инструмента.
//   - одна папка — она;
//   - repo задан — папка с этим именем;
//   - repo пуст — папка, на которую указывают пути (hints);
//   - иначе все папки, если all, и ошибка с перечнем папок, если не all.
func (r *Registry) gitTargets(repo string, hints []string, all bool) ([]gitTarget, error) {
	fs, err := r.folders()
	if err != nil {
		return nil, err
	}
	multi := len(fs) > 1
	mk := func(f workspace.Folder) gitTarget {
		return gitTarget{svc: r.Git.WithFolder(f.Path), folder: f, multi: multi}
	}
	if !multi {
		return []gitTarget{mk(fs[0])}, nil
	}
	repo = strings.TrimSpace(repo)
	if repo == "" {
		for _, h := range hints {
			name := folderHint(fs, h)
			if name == "" {
				continue
			}
			if repo != "" && !strings.EqualFold(repo, name) {
				return nil, fmt.Errorf("пути из разных папок (%s, %s) — коммитьте и смотрите git по одному репозиторию, укажите repo", repo, name)
			}
			repo = name
		}
	}
	if repo != "" {
		f, err := r.ws().FolderFor(repo)
		if err != nil {
			return nil, err
		}
		return []gitTarget{mk(f)}, nil
	}
	if all {
		out := make([]gitTarget, 0, len(fs))
		for _, f := range fs {
			out = append(out, mk(f))
		}
		return out, nil
	}
	names := make([]string, 0, len(fs))
	for _, f := range fs {
		names = append(names, f.Name)
	}
	return nil, fmt.Errorf("в рабочем пространстве несколько папок (%s): укажите repo — в какой из них выполнить git", strings.Join(names, ", "))
}

// gitEach прогоняет операцию по всем целям и подписывает вывод именем папки.
func gitEach(targets []gitTarget, op func(gitTarget) (string, error)) (string, error) {
	if len(targets) == 1 {
		return op(targets[0])
	}
	var b strings.Builder
	for i, t := range targets {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "## %s\n", t.folder.Name)
		out, err := op(t)
		switch {
		case err != nil && strings.Contains(strings.ToLower(err.Error()), "not a git repository"):
			b.WriteString("(не git-репозиторий)")
		case err != nil:
			fmt.Fprintf(&b, "error: %v", err)
		case strings.TrimSpace(out) == "":
			b.WriteString("(пусто)")
		default:
			b.WriteString(strings.TrimRight(out, "\n"))
		}
	}
	return b.String(), nil
}

// gitMv — git mv в репозитории, которому принадлежат оба пути. Ошибка означает
// «git не подошёл», вызывающий откатывается на обычное переименование.
func (r *Registry) gitMv(from, to string) error {
	ts, err := r.gitTargets("", []string{from, to}, false)
	if err != nil {
		return err
	}
	t := ts[0]
	if t.multi {
		fs, err := r.folders()
		if err != nil {
			return err
		}
		if !strings.EqualFold(folderHint(fs, from), t.folder.Name) || !strings.EqualFold(folderHint(fs, to), t.folder.Name) {
			return fmt.Errorf("пути в разных папках")
		}
	}
	_, err = t.svc.Mv(t.rel(from), t.rel(to))
	return err
}

// namedIndex — индекс символов одной папки. name пуст, если папка единственная:
// тогда пути остаются как есть.
type namedIndex struct {
	name string
	idx  *projindex.Index
}

func (r *Registry) projectIndexes() ([]namedIndex, error) {
	fs, err := r.folders()
	if err != nil {
		return nil, err
	}
	multi := len(fs) > 1
	out := make([]namedIndex, 0, len(fs))
	var firstErr error
	for _, f := range fs {
		idx, err := projindex.Default().Get(f.Path)
		if err != nil {
			if !multi {
				return nil, err
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		ni := namedIndex{idx: idx}
		if multi {
			ni.name = f.Name
		}
		out = append(out, ni)
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// searchIndexes ищет по индексам всех папок и склеивает результаты по кругу,
// чтобы одна шумная папка не вытеснила остальные.
func (r *Registry) searchIndexes(limit int, search func(*projindex.Index, int) []projindex.Hit) ([]projindex.Hit, error) {
	list, err := r.projectIndexes()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 25
	}
	if limit > 200 {
		limit = 200
	}
	per := make([][]projindex.Hit, 0, len(list))
	for _, ni := range list {
		hits := search(ni.idx, limit)
		if ni.name != "" {
			for i := range hits {
				hits[i].File = ni.name + "/" + hits[i].File
			}
		}
		per = append(per, hits)
	}
	if len(per) == 1 {
		return per[0], nil
	}
	var out []projindex.Hit
	for round := 0; len(out) < limit; round++ {
		added := false
		for _, hits := range per {
			if round < len(hits) && len(out) < limit {
				out = append(out, hits[round])
				added = true
			}
		}
		if !added {
			break
		}
	}
	return out, nil
}

// memoryResolver разворачивает пути из заметок памяти («api/main.go») в файлы на диске.
func (r *Registry) memoryResolver() projmem.Resolver {
	ws := r.ws()
	return func(ref string) string {
		p, err := ws.Resolve(ref)
		if err != nil {
			return ""
		}
		return p
	}
}

// folderOfPath — папка рабочего пространства, внутри которой лежит файл.
func folderOfPath(folders []workspace.Folder, full string) (workspace.Folder, bool) {
	best := workspace.Folder{}
	found := false
	for _, f := range folders {
		rel, err := filepath.Rel(f.Path, full)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if !found || len(f.Path) > len(best.Path) {
			best, found = f, true
		}
	}
	return best, found
}

// workspaceSummary — одной строкой: какие папки входят в рабочее пространство.
func workspaceSummary(folders []workspace.Folder) string {
	parts := make([]string, 0, len(folders))
	for _, f := range folders {
		parts = append(parts, fmt.Sprintf("%s=%s", f.Name, filepath.ToSlash(f.Path)))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
