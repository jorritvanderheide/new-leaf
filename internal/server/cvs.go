package server

import (
	"log"
	"net/http"
)

type cvRequest struct {
	Name   string   `json:"name"`
	Owners []string `json:"owners"`
}

// postCV creates a CV and answers with its ID; the editor then switches to it.
func (s *Server) postCV(w http.ResponseWriter, r *http.Request) {
	var req cvRequest
	if err := readJSON(r, &req); err != nil {
		httpError(w, err)
		return
	}
	id, err := s.auth.CVs.Create(req.Name, req.Owners)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]string{"id": id})
}

func (s *Server) putCV(r *http.Request, user string) error {
	var req cvRequest
	if err := readJSON(r, &req); err != nil {
		return err
	}
	return s.auth.CVs.Update(r.PathValue("id"), req.Name, req.Owners)
}

// deleteCV moves a CV to the trash and takes its share links offline.
func (s *Server) deleteCV(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	st := s.state(id)
	st.publish.Lock()
	st.content.Lock()
	err := s.auth.CVs.Delete(id)
	st.content.Unlock()
	st.publish.Unlock()
	if err != nil {
		httpError(w, err)
		return
	}
	if s.sharing {
		if err := s.reconcile(); err != nil {
			log.Printf("reconcile: %v", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
