package server

import (
	"fmt"
	"io/fs"
	"net/http"

	"github.com/rakunlabs/ada"
	"github.com/rakunlabs/ada/handler/folder"
	ui "github.com/rakunlabs/calendar/_ui"
)

func registerUI(s *ada.Mux, prefix string) error {
	dist, err := fs.Sub(ui.Dist, "dist")
	if err != nil {
		return fmt.Errorf("embedded UI: %w", err)
	}

	files, err := folder.New(&folder.Config{
		PrefixPath: prefix, Index: true, StripIndexName: true, SPA: true,
		CacheRegex: []*folder.RegexCacheStore{
			{Regex: `^index\.html$`, CacheControl: "no-cache"},
			{Regex: `.*\.(js|css|woff2)$`, CacheControl: "public, max-age=31536000, immutable"},
		},
	})
	if err != nil {
		return fmt.Errorf("UI folder handler: %w", err)
	}

	files.SetFs(http.FS(dist))
	s.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		files.ServeHTTP(w, r)
	})

	return nil
}
