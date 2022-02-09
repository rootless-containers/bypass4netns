package router

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/rootless-containers/bypass4netns/pkg/api"
	"github.com/rootless-containers/bypass4netns/pkg/bypass4netns"
)

type Backend struct {
	BypassDriver BypassDriver
}

type BypassDriver interface {
	ListBypass() []bypass4netns.BypassStatus
	StartBypass(*bypass4netns.BypassSpec) (*bypass4netns.BypassStatus, error)
	StopBypass(id string) error
}

func (b *Backend) onError(w http.ResponseWriter, r *http.Request, err error, ec int) {
	w.WriteHeader(ec)
	w.Header().Set("Content-Type", "application/json")
	// it is safe to return the err to the client, because the client is reliable
	e := api.ErrorJSON{
		Message: err.Error(),
	}
	_ = json.NewEncoder(w).Encode(e)
}

func (b *Backend) GetBypasses(w http.ResponseWriter, r *http.Request) {
	bs := b.BypassDriver.ListBypass()
	m, err := json.Marshal(bs)
	if err != nil {
		b.onError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(m)
}

func (b *Backend) PostBypass(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var bSpec bypass4netns.BypassSpec
	if err := decoder.Decode(&bSpec); err != nil {
		b.onError(w, r, err, http.StatusBadRequest)
		return
	}
	bypassStatus, err := b.BypassDriver.StartBypass(&bSpec)
	if err != nil {
		b.onError(w, r, err, http.StatusBadRequest)
		return
	}
	m, err := json.Marshal(bypassStatus)
	if err != nil {
		b.onError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write(m)
}

func (b *Backend) DeleteBypass(w http.ResponseWriter, r *http.Request) {
	id, ok := mux.Vars(r)["id"]
	if !ok {
		b.onError(w, r, errors.New("id not specified"), http.StatusBadRequest)
		return
	}
	if err := b.BypassDriver.StopBypass(id); err != nil {
		b.onError(w, r, err, http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func AddRoutes(r *mux.Router, b *Backend) {
	v1 := r.PathPrefix("/v1").Subrouter()
	v1.Path("/bypass").Methods("GET").HandlerFunc(b.GetBypasses)
	v1.Path("/bypass").Methods("POST").HandlerFunc(b.PostBypass)
	v1.Path("/bypass/{id}").Methods("DELETE").HandlerFunc(b.DeleteBypass)
}
