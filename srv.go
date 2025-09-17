package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
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
	srv            *http.Server
	hmc            *HMC
	ctx            context.Context
	tls            bool
	certKEY        string
	certCRT        string
	mgmConsole     *ManagementConsole
	mgmcNextUpdate time.Time
	mgmcInterval   time.Duration
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
		IdleTimeout:  120 * time.Second,
	}
	s.mgmConsole = nil
	interval := config.GetString("hmc_mgms_retrieve_interval")
	if interval == "" {
		interval = "10m"
	}
	intervalD, err := time.ParseDuration(interval)
	if err != nil {
		log.Warnf("Error parsing hmc_mgms_retrieve_interval. Used 10m as a default value. err=%s", err)
		intervalD = 10 * time.Minute
	}
	s.mgmcInterval = intervalD
	s.mgmcNextUpdate = time.Now()

	if strings.ToLower(config.GetString("server_use_TLS")) == "yes" {
		s.tls = true
		// Configure TLS
		tlsConfig := &tls.Config{
			MinVersion:               tls.VersionTLS12,
			PreferServerCipherSuites: true,
			CurvePreferences: []tls.CurveID{
				tls.CurveP256,
				tls.X25519,
			},
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			},
		}
		s.srv.TLSConfig = tlsConfig

		// use middleware to add security header
		// about middleware read here: https://github.com/gorilla/mux?tab=readme-ov-file#middleware
		router.Use(securityHeadersMiddleware)

		// Create auth middleware
		auth := NewAuthMiddleware(config)
		if auth != nil {
			router.Use(auth.Middleware)
		}

	} else {
		s.tls = false
	}
	s.certKEY = config.GetString("server_key")
	s.certCRT = config.GetString("server_crt")

	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()

	err := hmc.Logon(ctx)
	if err != nil {
		log.Errorf("Serv init. No connection to HMC. %s", err)
	}
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security headers
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		next.ServeHTTP(w, r)
	})
}

func (s *Srv) Run(c chan error) {
	if s.tls {
		log.Infof("Srv Running secure HTTPS. Listening: %s", s.srv.Addr)
		c <- s.srv.ListenAndServeTLS(s.certCRT, s.certKEY)
	} else {
		log.Infof("Srv Running. Listening: %s", s.srv.Addr)
		c <- s.srv.ListenAndServe()
	}
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

func healthCheck(w http.ResponseWriter, r *http.Request) {
	respondWithJSON(w, http.StatusOK, map[string]string{"Server_status": "OK"})
}

func getClientIP(r *http.Request) string {
	// Try various headers that might contain the real client IP
	headers := []string{
		"X-Real-Ip",
		"X-Forwarded-For",
		"X-Client-Ip",
		"CF-Connecting-IP", // Cloudflare
	}

	for _, header := range headers {
		if ip := r.Header.Get(header); ip != "" {
			return ip
		}
	}

	// Fallback to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func tlsVersionToString(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("Unknown (%x)", version)
	}
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
		UUID string `json:"uuid"`
		HMC  string `json:"hmc"`
		//HMCmtms   string `json:"hmc_mtms"`
		MTMS          string `json:"mtms"`
		SysName       string `json:"systemname"`
		State         string `json:"state"`
		LED           bool   `json:"led"`
		RefCode       string `json:"rfc"`
		MergedRefCode string `json:"mrfc"`
		Location      string `json:"location"`
		//Timestamp int64  `json:"timestamp"`
		Elapsed int64 `json:"elapsed"`
	}
	type RespJson struct {
		HMC     string `json:"hmc"`
		HMCmtms string `json:"hmc_mtms"`
		//HMCuuid   string      `json:"hmc_uuid"`
		//Timestamp int64       `json:"timestamp"`
		Elapsed int64       `json:"elapsed"`
		Systems []QuickMgms `json:"systems"`
	}

	globalStart := time.Now()

	ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
	defer cancel()

	myname := "quickManagedSystem"
	hmc := s.hmc

	ip_tls := getClientIP(r)
	if s.tls {
		ip_tls = fmt.Sprintf("%s (%s)", ip_tls, tlsVersionToString(r.TLS.Version))
	}
	log.Infof("%s, hmc: %s, connection from: %s", myname, hmc.hmcName, ip_tls)

	var mgmConsole *ManagementConsole

	if (s.mgmConsole == nil) || s.mgmcNextUpdate.Before(time.Now()) {
		mgmConsole, err := hmc.GemManagementConsoleData(ctx)
		if err != nil {
			log.Errorf("%s, calling GemManagementConsoleData err=%s", myname, err)
			respondWithJSON(w, http.StatusInternalServerError, map[string]string{"result": "getManagementConsoleData error"})
			return
		}
		s.mgmConsole = mgmConsole
		s.mgmcNextUpdate = time.Now().Add(s.mgmcInterval)
	} else {
		mgmConsole = s.mgmConsole
	}

	totServers := len(mgmConsole.Links)

	respJson := RespJson{}

	respJson.HMC = hmc.hmcName
	respJson.HMCmtms = mgmConsole.HMCType + "-" + mgmConsole.HMCMod + "*" + mgmConsole.HMCSerial
	//respJson.HMCuuid = mgmConsole.ID
	//respJson.Timestamp = time.Now().Unix()
	respJson.Elapsed = 0
	respJson.Systems = []QuickMgms{}

	//var system QuickMgms

	for num, elem := range mgmConsole.Links {

		system := QuickMgms{
			UUID: "",
			HMC:  respJson.HMC,
			//HMCmtms:   respJson.HMCmtms,
			MTMS:          "",
			SysName:       "",
			State:         "",
			LED:           false,
			RefCode:       "",
			MergedRefCode: "",
			Location:      "",
			//Timestamp: 0,
			Elapsed: 0,
		}

		serverStart := time.Now()

		a := strings.Split(elem.Href, "/")
		uuid := a[len(a)-1]
		system.UUID = uuid

		jsonData, err := hmc.GetMgmsQuick(ctx, uuid)
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

		//if value, exists = mapData["MTMS"]; exists {
		//	system.MTMS = assertString(value)
		//}
		if value, exists = mapData["MTMS"]; exists {
			mtms := assertString(value)
			mtm, s, found := strings.Cut(mtms, "*")
			if found {
				system.MTMS = mtm + "-" + s
			} else {
				system.MTMS = mtms
			}
		}
		if value, exists = mapData["SystemName"]; exists {
			system.SysName = assertString(value)
		}
		if value, exists = mapData["State"]; exists {
			system.State = assertString(value)
		}
		if value, exists = mapData["SystemLocation"]; exists {
			system.Location = assertString(value)
		}
		if value, exists = mapData["PhysicalSystemAttentionLEDState"]; exists {
			str = assertString(value)
			if str == "null" {
				str = "false"
			}
			if str == "false" {
				system.LED = false
			} else {
				system.LED = true
			}
			//system.LED = str
		}
		if value, exists = mapData["ReferenceCode"]; exists {
			system.RefCode = assertString(value)
		}
		if value, exists = mapData["MergedReferenceCode"]; exists {
			system.MergedRefCode = assertString(value)
		}
		//system.Timestamp = time.Now().Unix()
		system.Elapsed = int64(time.Since(serverStart)) / 1000000

		log.Debugf("%s ---> %s %3d/%d: %s", myname, hmc.hmcName, num+1, totServers, system.MTMS)

		respJson.Systems = append(respJson.Systems, system)

	}

	respJson.Elapsed = int64(time.Since(globalStart)) / 1000000
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
