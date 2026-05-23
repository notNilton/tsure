// Package render encapsula a renderizacao de templates HTML do tsure e
// permite hot-reload em desenvolvimento  os handlers conversam com a
// interface Executor; em dev usa-se Reloader (le do disco quando o
// arquivo muda), em prod usa-se *html/template.Template embutido.
package render

import (
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Executor e o contrato consumido pelos handlers. Tanto *template.Template
// quanto *Reloader o satisfazem.
type Executor interface {
	ExecuteTemplate(w io.Writer, name string, data any) error
}

// Reloader re-parsa os templates do disco somente quando ha mudancas
// (compara mtime do arquivo mais novo). Da hot-reload de .html sem
// reiniciar o servidor e sem pagar reparse em todo request.
type Reloader struct {
	mu        sync.RWMutex
	tmpl      *template.Template
	lastMtime time.Time

	funcs    template.FuncMap
	root     string   // ex: "apps/web/templates"
	patterns []string // ex: []string{"*.html", "partials/*.html"}
}

// NewReloader carrega os templates iniciais. O CWD precisa resolver root.
func NewReloader(root string, patterns []string, funcs template.FuncMap) (*Reloader, error) {
	r := &Reloader{root: root, patterns: patterns, funcs: funcs}
	if err := r.reload(true); err != nil {
		return nil, err
	}
	return r, nil
}

// reload re-le do disco. Se force=false, so reparsa quando algum arquivo
// tem mtime mais recente que a ultima carga.
func (r *Reloader) reload(force bool) error {
	files, maxMtime, err := r.scan()
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("nenhum template casado em %s (%v)", r.root, r.patterns)
	}

	r.mu.RLock()
	stale := force || maxMtime.After(r.lastMtime)
	r.mu.RUnlock()
	if !stale {
		return nil
	}

	t, err := template.New("").Funcs(r.funcs).ParseFiles(files...)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	r.mu.Lock()
	r.tmpl = t
	r.lastMtime = maxMtime
	r.mu.Unlock()
	return nil
}

func (r *Reloader) scan() ([]string, time.Time, error) {
	var files []string
	var maxMtime time.Time
	for _, p := range r.patterns {
		matches, err := filepath.Glob(filepath.Join(r.root, p))
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("glob %s: %w", p, err)
		}
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil {
				continue
			}
			if info.ModTime().After(maxMtime) {
				maxMtime = info.ModTime()
			}
			files = append(files, m)
		}
	}
	return files, maxMtime, nil
}

// ExecuteTemplate satisfaz Executor.
func (r *Reloader) ExecuteTemplate(w io.Writer, name string, data any) error {
	if err := r.reload(false); err != nil {
		log.Printf("template reload (mantendo anterior): %v", err)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tmpl.ExecuteTemplate(w, name, data)
}

// LoadEmbedded parseia templates do embed.FS  uso em prod.
func LoadEmbedded(embedded fs.FS, patterns []string, funcs template.FuncMap) (*template.Template, error) {
	return template.New("").Funcs(funcs).ParseFS(embedded, patterns...)
}
