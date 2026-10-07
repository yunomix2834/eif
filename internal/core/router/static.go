package router

import (
	"io/fs"
	"mime"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func createStaticFallback(
	staticDir string,
	embedded fs.FS,
) gin.HandlerFunc {
	root, _ := filepath.Abs(staticDir)

	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet &&
			c.Request.Method != http.MethodHead {
			c.Status(http.StatusNotFound)
			return
		}

		if strings.HasPrefix(
			c.Request.URL.Path,
			"/api/",
		) {
			c.Status(http.StatusNotFound)
			return
		}

		requestPath := strings.TrimPrefix(
			pathpkg.Clean("/"+c.Request.URL.Path),
			"/",
		)
		if requestPath == "." {
			requestPath = ""
		}

		candidates := buildStaticCandidates(requestPath)

		// Disk first: useful for local development and explicit overrides.
		for _, name := range candidates {
			candidate := filepath.Join(
				root,
				filepath.FromSlash(name),
			)
			candidateAbs, err := filepath.Abs(candidate)
			if err != nil || !isPathWithinRoot(
				root,
				candidateAbs,
			) {
				continue
			}
			info, err := os.Stat(candidateAbs)
			if err == nil && !info.IsDir() {
				c.File(candidateAbs)
				return
			}
		}

		// Release binary fallback: serves the Next.js static export compiled by
		// go:embed. This makes eif / eif.exe runnable without a web/static folder.
		for _, name := range candidates {
			if serveEmbedded(
				c,
				embedded,
				name,
			) {
				return
			}
		}

		// Keep the previous SPA-style fallback behavior.
		if index := filepath.Join(
			root,
			"index.html",
		); isPathWithinRoot(
			root,
			index,
		) {
			if info, err := os.Stat(index); err == nil && !info.IsDir() {
				c.File(index)
				return
			}
		}
		if serveEmbedded(
			c,
			embedded,
			"index.html",
		) {
			return
		}

		c.Status(http.StatusNotFound)
	}
}

func buildStaticCandidates(requestPath string) []string {
	if requestPath == "" {
		return []string{"index.html"}
	}

	candidates := []string{
		requestPath,
		pathpkg.Join(
			requestPath,
			"index.html",
		),
	}
	if pathpkg.Ext(requestPath) == "" {
		candidates = append(
			candidates,
			requestPath+".html",
		)
	}
	return candidates
}

func serveEmbedded(
	c *gin.Context,
	root fs.FS,
	name string,
) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}

	data, err := fs.ReadFile(
		root,
		name,
	)
	if err != nil {
		return false
	}

	contentType := mime.TypeByExtension(pathpkg.Ext(name))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}

	c.Header(
		"Content-Type",
		contentType,
	)
	c.Header(
		"Content-Length",
		strconv.Itoa(len(data)),
	)
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return true
	}
	c.Data(
		http.StatusOK,
		contentType,
		data,
	)
	return true
}

// isPathWithinRoot prevents path traversal when an on-disk static directory is used.
func isPathWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(
		root,
		candidate,
	)
	if err != nil {
		return false
	}
	return rel != ".." &&
		!strings.HasPrefix(
			rel,
			".."+string(filepath.Separator),
		)
}
