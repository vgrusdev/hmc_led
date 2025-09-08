package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	//"bytes"

	//"io"

	"github.com/gorilla/mux"
	"github.com/spf13/viper"

	"context"
	//"errors"
)

type Srv struct {
	//router 	*mux.Router
	srv *http.Server
	hmc *HMC
	ctx context.Context
}

func (s *Srv) SrvInit(ctx context.Context, config *viper.Viper, hmc *HMC) {

	router := mux.NewRouter()
	router.HandleFunc("/health", HealthCheck).Methods("GET")
	//router.HandleFunc("/alert", s.Alert).Methods("POST") // Use per-Alert annotation, labels, images

	s.ctx = ctx
	s.hmc = hmc
	s.srv = &http.Server{
		Handler:      router,
		Addr:         config.GetString("srv_addr") + ":" + config.GetString("srv_port"),
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}
}

func HealthCheck(w http.ResponseWriter, r *http.Request) {
	respondWithJSON(w, http.StatusOK, map[string]string{"result": "success"})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

func (s *Srv) Run(c chan string) {
	slog.Info("Running", "Listen:", s.srv.Addr)

	if err := s.srv.ListenAndServe(); err != nil {
		c <- fmt.Sprintf("%s", err)
	} else {
		c <- "OK"
	}
	close(c)
}

func (s *Srv) Shutdown(ctx context.Context, c chan string) {
	slog.Info("Srv shutting down..")
	err := s.srv.Shutdown(ctx)
	c <- fmt.Sprintf("Server shutdown %s", err)
	close(c)
}
