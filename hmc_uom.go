package main

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	//"os"
	//"os/signal"
	//"sync/atomic"
	//"syscall"
	"strings"
	"time"

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

	if hmc.connected {
		//slog.Error("Attempting to login when connected")
		return fmt.Errorf("Attempting to login when connected")
	}

	url := "https://" + hmc.hmcHostname + ":12443/rest/api/web/Logon"
	payload := "<LogonRequest schemaVersion=\"V1_0\" xmlns=\"http://www.ibm.com/xmlns/systems/power/firmware/web/mc/2012_10/\" " +
		"xmlns:mc=\"http://www.ibm.com/xmlns/systems/power/firmware/web/mc/2012_10/\">" +
		"<UserID>" + hmc.user + "</UserID><Password>" + hmc.passwd + "</Password></LogonRequest>"

	fmt.Printf("url: %s\npayload: %s\n", url, payload)
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

	fmt.Println("Logon:")
	fmt.Printf("Status:%s, %d\n", resp.Status, resp.StatusCode)
	fmt.Printf("Header:%v\n", resp.Header)

	if resp.StatusCode != 200 {
		return fmt.Errorf("Logon failed error code: %s url: %s\n", resp.Status, url)
	}
	// Parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	fmt.Printf("Body: %s\n", body)

	type LogonResponse struct {
		Token string `xml:"X-API-Session"`
	}
	var response LogonResponse
	if err := xml.Unmarshal([]byte(body), &response); err != nil {
		return fmt.Errorf("failed to parse XML: %w", err)
	}

	fmt.Printf("Token: %s\n", response.Token)

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

	fmt.Println("Logoff:")
	fmt.Printf("Status:%s, %d\n", resp.Status, resp.StatusCode)
	fmt.Printf("Header:%v\n", resp.Header)

	if resp.StatusCode != 200 && resp.StatusCode != 202 && resp.StatusCode != 204 {
		// Parse response
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Logoff failed error code: %s, url: %s, body:%s\n", resp.Status, url, body)
	}

	hmc.token = ""
	hmc.connected = false
	return nil
}

func (hmc *HMC) Shutdown(ctx context.Context, c chan string) {

	slog.Info("HMC Logoff and connection shutting down..")
	if err := hmc.Logoff(ctx); err != nil {
		c <- fmt.Sprintf("HMC Logoff and shutdown %s", err)
	} else {
		c <- "OK"
	}
	close(c)
}

func (hmc *HMC) CloseIdleConnections() {
	//hmc.Logoff()
	hmc.client.CloseIdleConnections()
}
