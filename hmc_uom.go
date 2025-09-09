package main

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"

	//"log/slog"
	"net/http"
	"sync"

	//"os"
	//"os/signal"
	//"sync/atomic"
	//"syscall"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type HMC struct {
	client      *http.Client
	hmcName     string
	hmcHostname string
	hmcUuid     string
	baseURL     string
	user        string
	passwd      string
	token       string
	connected   bool
}

func NewHMC(config *viper.Viper) *HMC {

	var tls_skip_verify bool

	if config.GetString("tls_skip_verify") == "yes" {
		tls_skip_verify = true
	} else {
		tls_skip_verify = false
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: tls_skip_verify, // HMC appears not to have a genuine recognised CA certficate
		},
		MaxIdleConns:        1,
		MaxIdleConnsPerHost: 1,
		IdleConnTimeout:     60 * time.Second,
		DisableKeepAlives:   false, // Explicitly enable keep-alive
	}

	hmc := HMC{
		client: &http.Client{
			Transport: transport,
		},
		hmcName:     config.GetString("hmc_name"),
		hmcHostname: config.GetString("hmc_hostname"),
		//baseURL:	"https://" + hmcHostname + ":12443/rest/api"
		user:      config.GetString("hmc_user"),
		passwd:    config.GetString("hmc_passwd"),
		connected: false,
	}

	return &hmc
}

func (hmc *HMC) Logon(ctx context.Context) error {

	//if hmc.connected {
	//	//slog.Error("Attempting to login when connected")
	//	return fmt.Errorf("Attempting to login when connected")
	//}

	url := "https://" + hmc.hmcHostname + ":12443/rest/api/web/Logon"
	payload := "<LogonRequest schemaVersion=\"V1_0\" xmlns=\"http://www.ibm.com/xmlns/systems/power/firmware/web/mc/2012_10/\" " +
		"xmlns:mc=\"http://www.ibm.com/xmlns/systems/power/firmware/web/mc/2012_10/\">" +
		"<UserID>" + hmc.user + "</UserID><Password>" + hmc.passwd + "</Password></LogonRequest>"

	log.Debugf("hmc.Logon: url: %s\npayload: %s", url, payload)
	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, strings.NewReader(payload))
	if err != nil {
		return err
	}

	// Set headers
	req.Header.Set("Content-Type", "application/vnd.ibm.powervm.web+xml; type=LogonRequest")
	//req.Header.Set("Accept", "application/xml")
	req.Header.Set("Connection", "keep-alive")

	// Execute request
	resp, err := hmc.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	log.Debugln("Logon:")
	log.Infof("Logon status:%s, %d", resp.Status, resp.StatusCode)
	log.Debugf("Header:%v\n", resp.Header)

	if resp.StatusCode != 200 {
		return fmt.Errorf("Logon failed error code: %s url: %s\n", resp.Status, url)
	}
	// Parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	log.Debugf("Body: %s\n", body)

	type LogonResponse struct {
		Token string `xml:"X-API-Session"`
	}
	var response LogonResponse
	if err := xml.Unmarshal([]byte(body), &response); err != nil {
		return fmt.Errorf("failed to parse XML: %w", err)
	}

	log.Debugf("Token: %s\n", response.Token)

	hmc.token = response.Token
	hmc.connected = true
	return nil
}

func (hmc *HMC) Logoff(ctx context.Context) error {

	if !hmc.connected {
		//slog.Error("Attempting to login when connected")
		return fmt.Errorf("Attempting to logoff when not connected")
	}

	url := "https://" + hmc.hmcHostname + ":12443/rest/api/web/Logon"
	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	// Set headers
	req.Header.Set("X-API-Session", hmc.token)

	// Execute request
	resp, err := hmc.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	log.Debugln("Logoff:")
	log.Infof("Logoff status:%s, %d", resp.Status, resp.StatusCode)
	log.Debugf("Header:%v\n", resp.Header)

	if resp.StatusCode != 200 && resp.StatusCode != 202 && resp.StatusCode != 204 {
		// Parse response
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Logoff failed error code: %s, url: %s, body:%s\n", resp.Status, url, body)
	}

	hmc.token = ""
	hmc.connected = false
	return nil
}

func (hmc *HMC) Shutdown(ctx context.Context, wg *sync.WaitGroup) {

	defer wg.Done()

	log.Infoln("HMC shutting down..")
	if err := hmc.Logoff(ctx); err != nil {
		log.Warnf("HMC shutdown: %s", err)
	} else {
		log.Infoln("HMC shutdown OK")
	}
}

func (hmc *HMC) CloseIdleConnections() {
	//hmc.Logoff()
	log.Infoln("HMC closing idle connections.")
	hmc.client.CloseIdleConnections()
}
