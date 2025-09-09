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
		//MaxIdleConns:        1,
		//MaxIdleConnsPerHost: 1,
		//IdleConnTimeout:     60 * time.Second,
		//DisableKeepAlives:   false, // Explicitly enable keep-alive
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
		//log.Errorln("Attempting to login when connected")
		return fmt.Errorf("Attempting to logon when already connected")
	}

	hmc.token = ""
	hmc.connected = false

	url := "https://" + hmc.hmcHostname + ":12443/rest/api/web/Logon"
	payload := "<LogonRequest schemaVersion=\"V1_0\" xmlns=\"http://www.ibm.com/xmlns/systems/power/firmware/web/mc/2012_10/\" " +
		"xmlns:mc=\"http://www.ibm.com/xmlns/systems/power/firmware/web/mc/2012_10/\">" +
		"<UserID>" + hmc.user + "</UserID><Password>" + hmc.passwd + "</Password></LogonRequest>"

	//log.Debugf("hmc.Logon: url: %s\npayload: %s", url, payload)
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

	//log.Debugln("Logon:")
	log.Infof("Logon status:%s, %d", resp.Status, resp.StatusCode)
	//log.Debugf("Logon Header:%v\n", resp.Header)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("Logon failed error code: %s url: %s\n", resp.Status, url)
	}
	//log.Debugf("Body: %s\n", body)

	// Parse logon response to get the token
	type LogonResponse struct {
		Token string `xml:"X-API-Session"`
	}
	var response LogonResponse
	if err := xml.Unmarshal([]byte(body), &response); err != nil {
		return fmt.Errorf("Logon failed to parse XML: %w", err)
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

	log.Infof("Logoff status:%s, %d", resp.Status, resp.StatusCode)
	//log.Debugf("Header:%v\n", resp.Header)

	body, _ := io.ReadAll(resp.Body)
	log.Debugf("Logoff Body: %s", body)

	if resp.StatusCode != 200 && resp.StatusCode != 202 && resp.StatusCode != 204 {
		return fmt.Errorf("Logoff failed error code: %s, url: %s", resp.Status, url)
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

func (hmc *HMC) GetInfoByUrl(ctx context.Context, urlPath string, headers map[string]string) ([]byte, error) {

	myname := "hmc.getInfoByUrl"

	log.Debugf("%s urlPath=%s, header=%s", myname, urlPath, headers)

	if !hmc.connected {
		log.Infof("%s not connected. Trying to logon", myname)
		if err := hmc.Logon(ctx); err != nil {
			return []byte{}, fmt.Errorf("%s Not connected. Logon error: %w", myname, err)
		}
	}

	//url := fmt.Sprintf("https://%s:12443%s", hmc.hmcHostname, urlPath) // urlPath - absolute path starting with /
	//url := "https://" + hmc.hmcHostname + ":12443" + urlPath
	url := "https://" + hmc.hmcHostname + ":12443/rest/api/uom/ManagementConsole"

	//req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)

	if err != nil {
		return []byte{}, fmt.Errorf("%s %w", myname, err)
	}

	// Set headers
	req.Header.Set("X-API-Session", hmc.token)
	// Set custom headers
	//for key, value := range headers {
	//	req.Header.Set(key, value)
	//}
	fmt.Printf("Request:%s\n", req)
	// Execute request
	resp, err := hmc.client.Do(req)
	if err != nil {
		return []byte{}, err
	}
	defer resp.Body.Close()
	body, errBody := io.ReadAll(resp.Body)

	log.Debugf("%s status:%s, %d", myname, resp.Status, resp.StatusCode)
	//log.Debugf("Header:%v\n", resp.Header)
	//log.Debugf("Body: %s\n", body)

	if resp.StatusCode == 200 {
		return body, errBody
	} else if resp.StatusCode == 204 {
		return []byte{}, nil
	} else if resp.StatusCode == 401 || resp.StatusCode == 403 {

		// try to logon once again
		log.Infof("%s not connected by responce. Trying to logon once again", myname)
		if err := hmc.Logon(ctx); err == nil {
			//resp.Body.Close()
			req.Header.Set("X-API-Session", hmc.token)
			resp, err := hmc.client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				body, errBody := io.ReadAll(resp.Body)

				log.Debugf("%s status:%s, %d", myname, resp.Status, resp.StatusCode)
				//log.Debugf("Header:%v\n", resp.Header)
				//log.Debugf("Body: %s\n", body)

				if resp.StatusCode == 200 {
					return body, errBody
				} else if resp.StatusCode == 204 {
					return []byte{}, nil
				} else {
					return []byte{}, fmt.Errorf("%s response status: %s, url: %s", myname, resp.Status, url)
				}
			}
		}
	}

	return []byte{}, fmt.Errorf("%s response status: %s, url: %s", myname, resp.Status, url)
}
