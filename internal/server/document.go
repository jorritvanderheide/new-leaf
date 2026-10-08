package server

import (
	"context"
	"path/filepath"

	"codeberg.org/BW20/new-leaf/internal/cv"
)

// document lays out a user's CV for print options; photo is the path of the
// profile photo.
func (s *Server) document(user string, opt cv.PrintOptions) (doc cv.Document, photo string, err error) {
	profile, err := s.store.Profile(user)
	if err != nil {
		return cv.Document{}, "", err
	}
	items, err := s.store.Items(user)
	if err != nil {
		return cv.Document{}, "", err
	}
	photo = s.store.PhotoPath(user)
	return cv.BuildDocument(profile, items, opt, filepath.Base(photo)), photo, nil
}

// renderPDF makes a PDF of a user's CV.
func (s *Server) renderPDF(ctx context.Context, user string, opt cv.PrintOptions) ([]byte, error) {
	doc, photo, err := s.document(user, opt)
	if err != nil {
		return nil, err
	}
	return s.typst.PDF(ctx, doc, photo)
}
