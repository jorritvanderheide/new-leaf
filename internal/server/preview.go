package server

import (
	"errors"
	"math"
	"net/http"
	"strconv"

	"codeberg.org/BW20/new-leaf/internal/cv"
	"codeberg.org/BW20/new-leaf/internal/render"
)

// postPDF renders a selection. The editor shows it as the preview and
// reads the page count from X-Page-Count.
func (s *Server) postPDF(w http.ResponseWriter, r *http.Request) {
	user := userOf(r)
	var req cv.PrintOptions
	if err := readJSON(r, &req); err != nil {
		httpError(w, err)
		return
	}
	if err := req.Validate(); err != nil {
		httpError(w, badRequest{err})
		return
	}
	pdf, err := s.renderPDF(r.Context(), user, req)
	if err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Page-Count", strconv.Itoa(render.PageCount(pdf)))
	w.Write(pdf)
}

type fitRequest struct {
	cv.PrintOptions
	Pages int `json:"pages"` // the page count to fit on
}

type fitResult struct {
	Spacing float64 `json:"spacing"`
	Pages   int     `json:"pages"` // pages at that spacing
	Fits    bool    `json:"fits"`  // false: even the tightest spacing needs more pages
}

// postFit finds the most generous spacing at which a selection still fits
// on the requested number of pages. More spacing never means fewer pages,
// so a binary search over the slider's steps needs about five renders.
func (s *Server) postFit(w http.ResponseWriter, r *http.Request) {
	user := userOf(r)
	var req fitRequest
	if err := readJSON(r, &req); err != nil {
		httpError(w, err)
		return
	}
	if err := req.Validate(); err != nil {
		httpError(w, badRequest{err})
		return
	}
	if req.Pages < 1 || req.Pages > 10 {
		httpError(w, badRequest{errors.New("pages must be between 1 and 10")})
		return
	}

	const step = 0.05
	steps := int(math.Round((cv.MaxSpacing - cv.MinSpacing) / step))
	spacing := func(i int) float64 { return math.Round((cv.MinSpacing+float64(i)*step)*100) / 100 }
	pagesAt := map[int]int{}
	count := func(i int) (int, error) {
		if n, ok := pagesAt[i]; ok {
			return n, nil
		}
		opt := req.PrintOptions
		opt.Spacing = spacing(i)
		pdf, err := s.renderPDF(r.Context(), user, opt)
		if err != nil {
			return 0, err
		}
		pagesAt[i] = render.PageCount(pdf)
		return pagesAt[i], nil
	}

	best := -1 // largest step that fits
	for lo, hi := 0, steps; lo <= hi; {
		mid := (lo + hi) / 2
		n, err := count(mid)
		if err != nil {
			httpError(w, err)
			return
		}
		if n <= req.Pages {
			best, lo = mid, mid+1
		} else {
			hi = mid - 1
		}
	}
	res := fitResult{Fits: best >= 0}
	if !res.Fits {
		best = 0 // as tight as it gets
	}
	res.Spacing = spacing(best)
	n, err := count(best)
	if err != nil {
		httpError(w, err)
		return
	}
	res.Pages = n
	writeJSON(w, res)
}

// postLayout tells where each item of a selection lands in its PDF, so the
// preview can make items clickable.
func (s *Server) postLayout(w http.ResponseWriter, r *http.Request) {
	user := userOf(r)
	var req cv.PrintOptions
	if err := readJSON(r, &req); err != nil {
		httpError(w, err)
		return
	}
	if err := req.Validate(); err != nil {
		httpError(w, badRequest{err})
		return
	}
	doc, photo, err := s.document(user, req)
	if err != nil {
		httpError(w, err)
		return
	}
	marks, err := s.typst.Layout(r.Context(), doc, photo)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, marks)
}
