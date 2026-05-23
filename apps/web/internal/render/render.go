// Package render encapsula a renderizacao de templates HTML do tsure e
// permite hot-reload em desenvolvimento  os handlers conversam com a
// interface Executor; em dev usa-se Reloader (le do disco a cada render),
// em prod usa-se diretamente *html/template.Template embutido.
package render

import (
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"path/filepath"
	"sync"
)

// Executor e o contrato consumido pelos handlers. Tanto *template.Template
// quanto *Reloader o satisfazem.
type Executor interface {
	ExecuteTemplate(w io.Writer, name string, data any) error
}

// Reloader re-parsa os templates do disco em todo Execute, dando hot-reload
// para edicoes em arquivos .html sem reiniciar o servidor.
type Reloader struct {
	mu       sync.RWMutex
	tmpl     *template.Template
	funcs    template.FuncMap
	root     string   // ex: "apps/web/templates"
	patterns []string // ex: []string{"*.html", "partials/*.html"}
}

// NewReloader carrega os templates iniciais do disco. O CWD precisa
// conseguir resolver root  ao rodar `go run ./apps/web` a partir da raiz
// do repo, root = "apps/web/templates".
func NewReloader(root string, patterns []string, funcs template.FuncMap) (*Reloader, error) {
	r := &Reloader{root: root, patterns: patterns, funcs: funcs}
	if err := r.reload(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Reloader) reload() error {
	t := template.New("").Funcs(r.funcs)
	var any bool
	for _, p := range r.patterns {
		matches, err := filepath.Glob(filepath.Join(r.root, p))
		if err != nil {
			return fmt.Errorf("glob %s: %w", p, err)
		}
		if len(matches) == 0 {
			continue
		}
		any = true
		if t, err = t.ParseFiles(matches...); err != nil {
			return fmt.Errorf("parse %s: %w", p, err)
		}
	}
	if !any {
		return fmt.Errorf("nenhum template casado em %s (%v)", r.root, r.patterns)
	}
	r.mu.Lock()
	r.tmpl = t
	r.mu.Unlock()
	return nil
}

// ExecuteTemplate satisfaz Executor. Em todo render tenta recarregar do
// disco; se o reload falhar (ex: arquivo sendo salvo), mantem o ultimo
// template valido e segue.
func (r *Reloader) ExecuteTemplate(w io.Writer, name string, data any) error {
	if err := r.reload(); err != nil {
		log.Printf("template reload (mantendo anterior): %v", err)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tmpl.ExecuteTemplate(w, name, data)
}

// LoadEmbedded parseia templates do embed.FS  uso recomendado em prod.
// Retorna *template.Template puro (ja satisfaz Executor).
func LoadEmbedded(embedded fs.FS, patterns []string, funcs template.FuncMap) (*template.Template, error) {
	return template.New("").Funcs(funcs).ParseFS(embedded, patterns...)
}
