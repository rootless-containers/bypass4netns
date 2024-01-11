package com

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/rootless-containers/bypass4netns/pkg/api"
)

type Backend struct {
	BypassDriver BypassDriver
}

type BypassDriver interface {
	ListInterfaces() map[string]ContainerInterfaces
	GetInterface(id string) *ContainerInterfaces
	PostInterface(id string, containerIfs *ContainerInterfaces)
	DeleteInterface(id string)
}

func AddRoutes(r *mux.Router, b *Backend) {
	v1 := r.PathPrefix("/v1").Subrouter()
	_ = v1
	v1.Path("/ping").Methods("GET").HandlerFunc(b.ping)
	v1.Path("/interfaces").Methods("GET").HandlerFunc(b.listInterfaces)
	v1.Path("/interface/{id}").Methods("GET").HandlerFunc(b.getInterface)
	v1.Path("/interface/{id}").Methods("POST").HandlerFunc(b.postInterface)
	v1.Path("/interface/{id}").Methods("DELETE").HandlerFunc(b.deleteInterface)
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

func (b *Backend) ping(w http.ResponseWriter, r *http.Request) {
	m, err := json.Marshal("pong")
	if err != nil {
		b.onError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(m)
}

func (b *Backend) listInterfaces(w http.ResponseWriter, r *http.Request) {
	ifs := b.BypassDriver.ListInterfaces()
	m, err := json.Marshal(ifs)
	if err != nil {
		b.onError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(m)
}

func (b *Backend) getInterface(w http.ResponseWriter, r *http.Request) {
	id, ok := mux.Vars(r)["id"]
	if !ok {
		b.onError(w, r, errors.New("id not specified"), http.StatusBadRequest)
		return
	}

	ifs := b.BypassDriver.GetInterface(id)
	if ifs == nil {
		b.onError(w, r, errors.New("not found"), http.StatusNotFound)
		return
	}

	m, err := json.Marshal(ifs)
	if err != nil {
		b.onError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(m)
}

func (b *Backend) postInterface(w http.ResponseWriter, r *http.Request) {
	id, ok := mux.Vars(r)["id"]
	if !ok {
		b.onError(w, r, errors.New("id not specified"), http.StatusBadRequest)
		return
	}

	decoder := json.NewDecoder(r.Body)
	var containerIfs ContainerInterfaces
	if err := decoder.Decode(&containerIfs); err != nil {
		b.onError(w, r, err, http.StatusBadRequest)
		return
	}
	b.BypassDriver.PostInterface(id, &containerIfs)

	ifs := b.BypassDriver.GetInterface(id)
	m, err := json.Marshal(ifs)
	if err != nil {
		b.onError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(m)
}

func (b *Backend) deleteInterface(w http.ResponseWriter, r *http.Request) {
	id, ok := mux.Vars(r)["id"]
	if !ok {
		b.onError(w, r, errors.New("id not specified"), http.StatusBadRequest)
		return
	}

	b.BypassDriver.DeleteInterface(id)
}
