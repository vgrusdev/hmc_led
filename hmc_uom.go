package main

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	//"os"
	//"os/signal"
	//"sync/atomic"
	//"syscall"
	"time"
	"strings"
)

type HMC struct {
	client		*http.Client
	hmcName		string
	hmcHostname	string
	hmcUuid		string
	baseURL		string
	user		string
	passwd		string
	token		string
	connected	bool
}

func NewHMC (hmcName, hmcHostname, user, passwd string) (*HMC) {

	transport := &http.Transport {
		TLSClientConfig:	&tls.Config {
				InsecureSkipVerify:	true,	// HMC appears not to have a genuine recognised CA certficate
		},
		MaxIdleConns:			1,
		MaxIdleConnsPerHost:	1,
		IdleConnTimeout:		60 * time.Second,
		DisableKeepAlives:		false,	// Explicitly enable keep-alive
	}

	hmc := HMC {
		client:		&http.Client{
					Transport: transport,
		},
		hmcName: 	hmcName,
		hmcHostname:hmcHostname,
		//baseURL:	"https://" + hmcHostname + ":12443/rest/api"
		user:		user,
		passwd:		passwd,
		connected:	false,
	}

	return &hmc
}

func (hmc *HMC) Logon (ctx context.Context) error {

	if hmc.connected {
		//slog.Error("Attempting to login when connected")
		return fmt.Errorf("Attempting to login when connected")
	}
	
	url := "https://" + hmc.hmcHostname + ":12443/rest/api/web/Logon"
	payload := 	"<LogonRequest schemaVersion=\"V1_0\" xmlns=\"http://www.ibm.com/xmlns/systems/power/firmware/web/mc/2012_10/\" " +
				"xmlns:mc=\"http://www.ibm.com/xmlns/systems/power/firmware/web/mc/2012_10/\">" +
				"<UserID>" + hmc.user + "</UserID><Password>" + hmc.passwd + "</Password></LogonRequest>"
 
	// Create request
	req, err := http.NewRequestWithContext(ctx, "PUT", url, strings.NewReader(payload))
	if err != nil {
		return err
	}

	// Set headers
	req.Header.Set("Content-Type", "application/vnd.ibm.powervm.web+xml; type=LogonRequest")
	//req.Header.Set("Accept", "application/xml")
	//req.Header.Set("Connection", "keep-alive")

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
		Token	string	`xml:"LogonResponse>X-API-Session"`
	}
	var response LogonResponse
	if err := xml.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("failed to parse XML: %w", err)
	}

	fmt.Printf("Token: %s\n", response.Token)
	return nil
}

func (hmc *HMC) Shutdown () {
	hmc.client.CloseIdleConnections()
}