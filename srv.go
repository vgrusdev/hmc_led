package main

import (
	"encoding/json"
	"strings"

	//"log/slog"
	"net/http"
	"sync"
	"time"

	//"bytes"

	//"io"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
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
	router.HandleFunc("/health", healthCheck).Methods("GET")
	router.HandleFunc("/status", s.status).Methods("GET")
	router.HandleFunc("/getManagementConsole", s.getManagementConsole).Methods("GET", "POST") //
	router.HandleFunc("/quickManagedSystem", s.quickManagedSystem).Methods("GET", "POST")     //

	s.ctx = ctx
	s.hmc = hmc
	s.srv = &http.Server{
		Handler:      router,
		Addr:         config.GetString("srv_addr") + ":" + config.GetString("srv_port"),
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()

	err := hmc.Logon(ctx)
	if err != nil {
		log.Errorf("Serv init. No connection to HMC. %s", err)
	}
}

func healthCheck(w http.ResponseWriter, r *http.Request) {

	respondWithJSON(w, http.StatusOK, map[string]string{"Server_status": "OK"})
}

func (s *Srv) status(w http.ResponseWriter, r *http.Request) {

	type Response struct {
		Srv                string `json:"server_status"`
		HMC                string `json:"hmc_connection"`
		LogonRequests      int64  `json:"logon_requests"`
		URLRequests        int64  `json:"url_requests"`
		MgmConsoleRequests int64  `json:"mgmconsole_requests"`
		QuickMgmsRequests  int64  `json:"quick_mgms_requests"`
	}
	hmc := s.hmc

	resp := Response{
		Srv:                "OK",
		HMC:                "",
		LogonRequests:      hmc.logon_requests,
		URLRequests:        hmc.url_requests,
		MgmConsoleRequests: hmc.mgmconsole_requests,
		QuickMgmsRequests:  hmc.quick_mgms_requests,
	}
	if hmc.connected {
		resp.HMC = "Connected"
	} else {
		resp.HMC = "Disconnected"
	}
	respondWithJSON(w, http.StatusOK, resp)
}
func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	w.Write(response)
}

func (s *Srv) Run(c chan error) {
	log.Infof("Srv Running. Listening: %s", s.srv.Addr)

	c <- s.srv.ListenAndServe()
	close(c)
}

func (s *Srv) Shutdown(ctx context.Context, wg *sync.WaitGroup) {

	defer wg.Done()

	log.Infoln("Srv shutting down..")
	if err := s.srv.Shutdown(ctx); err != nil {
		log.Warnf("Srv shutdown: %s", err)
	} else {
		log.Infoln("Srv shutdown: OK")
	}
}
func (s *Srv) getManagementConsole(w http.ResponseWriter, r *http.Request) {

	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	myname := "getManagementConsole"
	hmc := s.hmc
	mgmtConsole, err := hmc.GetManagementConsole(ctx)
	if err != nil {
		log.Errorf("%s: %s", myname, err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"result": "getManagementConsole error"})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	w.Write(mgmtConsole)
}

func (s *Srv) quickManagedSystem(w http.ResponseWriter, r *http.Request) {

	type QuickMgms struct {
		UUID      string `json:"uuid"`
		MTMS      string `json:"mtms"`
		SysName   string `json:"systemname"`
		State     string `json:"state"`
		LED       string `json:"led"`
		RefCode   string `json:"rfc"`
		Timestamp int64  `json:"timestamp"`
		Elapsed   int64  `json:"elapsed"`
	}
	type RespJson struct {
		HMC       string      `json:"hmc"`
		HMCmtms   string      `json:"hmc_mtms"`
		HMCuuid   string      `json:"hmc_uuid"`
		Timestamp int64       `json:"timestamp"`
		Elapsed   int64       `json:"elapsed"`
		Systems   []QuickMgms `json:"systems"`
	}

	globalStart := time.Now()

	ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
	defer cancel()

	myname := "quickManagedSystem"
	hmc := s.hmc
	log.Infof("%s hmc=%s", myname, hmc.hmcName)

	mgmConsole, err := hmc.GemManagementConsoleData(ctx)
	if err != nil {
		log.Errorf("%s, calling GemManagementConsoleData err=%s", myname, err)
		respondWithJSON(w, http.StatusInternalServerError, map[string]string{"result": "getManagementConsoleData error"})
		return
	}

	totServers := len(mgmConsole.Links)

	respJson := RespJson{}

	respJson.HMC = hmc.hmcName
	respJson.HMCmtms = mgmConsole.HMCType + "-" + mgmConsole.HMCMod + "*" + mgmConsole.HMCSerial
	respJson.HMCuuid = mgmConsole.ID
	respJson.Timestamp = time.Now().Unix()
	respJson.Elapsed = 0
	respJson.Systems = []QuickMgms{}

	var system QuickMgms

	for num, elem := range mgmConsole.Links {

		serverStart := time.Now()

		a := strings.Split(elem.Href, "/")
		system.UUID = a[len(a)-1]

		jsonData, err := hmc.GetMgmsQuick(ctx, system.UUID)
		if err != nil {
			log.Errorf("%s. GetMgmsQuick err=%s", myname, err)
			continue
		}
		mapData := make(map[string]interface{})
		err = json.Unmarshal([]byte(jsonData), &mapData)
		if err != nil {
			log.Errorf("%s. unmarshal error: %s", myname, err)
			continue
		}
		var value interface{}
		var exists bool
		var str string

		if value, exists = mapData["MTMS"]; exists {
			system.MTMS = assertString(value)
		}
		if value, exists = mapData["MTMS"]; exists {
			system.MTMS = assertString(value)
		}
		if value, exists = mapData["SystemName"]; exists {
			system.SysName = assertString(value)
		}
		if value, exists = mapData["State"]; exists {
			system.State = assertString(value)
		}
		if value, exists = mapData["PhysicalSystemAttentionLEDState"]; exists {
			str = assertString(value)
			if str == "null" {
				str = "false"
			}
			system.LED = str
		}
		if value, exists = mapData["ReferenceCode"]; exists {
			system.RefCode = assertString(value)
		}
		system.Timestamp = time.Now().Unix()
		system.Elapsed = int64(time.Since(serverStart))

		log.Debugf("%s ---> %s %3d/%d: %s", myname, hmc.hmcName, num+1, totServers, system.SysName)

		respJson.Systems = append(respJson.Systems, system)

	}

	respJson.Elapsed = int64(time.Since(globalStart))
	jsonData, _ := json.MarshalIndent(respJson, "", "  ")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	w.Write(jsonData)

}

func assertString(value interface{}) string {
	if str, ok := value.(string); ok {
		return str
	} else {
		return ""
	}
}
