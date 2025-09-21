package main

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"time"

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
	logon       *HMC_logon
	stats       *HMC_stats
}
type HMC_stats struct {
	logon_requests      int64
	url_requests        int64
	mgmconsole_requests int64
	quick_mgms_requests int64
}
type HMC_logon struct {
	connected bool
	token     string
	mu        sync.Mutex
}

type ManagementConsole struct {
	//XMLName   xml.Name `xml:"http://www.w3.org/2005/Atom feed"`
	ID        string     `xml:"entry>id"`
	HMCType   string     `xml:"entry>content>ManagementConsole>MachineTypeModelAndSerialNumber>MachineType"`
	HMCMod    string     `xml:"entry>content>ManagementConsole>MachineTypeModelAndSerialNumber>Model"`
	HMCSerial string     `xml:"entry>content>ManagementConsole>MachineTypeModelAndSerialNumber>SerialNumber"`
	HMCName   string     `xml:"entry>content>ManagementConsole>ManagementConsoleName"`
	Links     []SysLinks `xml:"entry>content>ManagementConsole>ManagedSystems>link"`
}
type SysLinks struct {
	Href string `xml:"href,attr"`
}

func NewHMC(config *viper.Viper) *HMC {

	hmc_logon := &HMC_logon{
		connected: false,
		token:     "",
	}

	hmc_stats := &HMC_stats{
		logon_requests:      0,
		url_requests:        0,
		mgmconsole_requests: 0,
		quick_mgms_requests: 0,
	}

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

	hmc := &HMC{
		client: &http.Client{
			Transport: transport,
		},
		hmcName:     config.GetString("hmc_name"),
		hmcHostname: config.GetString("hmc_hostname"),
		user:        config.GetString("hmc_user"),
		passwd:      config.GetString("hmc_passwd"),
		logon:       hmc_logon,
		stats:       hmc_stats,
		//connected:   false,
	}

	return hmc
}

// for debugging, when real HMC is not reachable
func readFileSafely(filename string) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		// Return empty slice instead of nil for consistency
		return []byte{}, err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return []byte{}, err
	}
	return data, nil
}

func (hmc *HMC) Logon(ctx context.Context, lock bool) error {

	hmc.stats.logon_requests++

	if lock {
		hmc.logon.mu.Lock()
		defer hmc.logon.mu.Unlock()
	}

	if hmc.logon.connected {
		log.Warnln("HMC Logon. Attempting to logon when already connected !")
		return nil
	}

	// reset connection status
	hmc.logon.token = ""
	//hmc.logon.connected = false		// it is already false if we are here - see "if.." above :)

	url := "https://" + hmc.hmcHostname + ":12443/rest/api/web/Logon"
	payload := "<LogonRequest schemaVersion=\"V1_0\" xmlns=\"http://www.ibm.com/xmlns/systems/power/firmware/web/mc/2012_10/\" " +
		"xmlns:mc=\"http://www.ibm.com/xmlns/systems/power/firmware/web/mc/2012_10/\">" +
		"<UserID>" + hmc.user + "</UserID><Password>" + hmc.passwd + "</Password></LogonRequest>"

	//log.Debugf("hmc.Logon: url: %s\npayload: %s", url, payload)
	// Create request with context. Suppose ctx - context with timeout...
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("HMC Logon Request. %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/vnd.ibm.powervm.web+xml; type=LogonRequest")
	//req.Header.Set("Accept", "application/xml")
	//req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Host", hmc.hmcHostname+":12443")
	req.Header.Set("Accept", "*/*")

	// Execute request
	resp, err := hmc.client.Do(req)
	if err != nil {
		return fmt.Errorf("HMC Logon Do. %w", err)
	}
	defer resp.Body.Close()

	log.Infof("HMC %s Logon status:%s", hmc.hmcName, resp.Status)
	//log.Debugf("Logon Header:%v\n", resp.Header)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("HMC Logon Body %w", err)
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
	log.Debugf("Token: %s", response.Token)

	hmc.logon.token = response.Token
	hmc.logon.connected = true
	return nil
}

func (hmc *HMC) Logoff(ctx context.Context, lock bool) error {

	if lock {
		hmc.logon.mu.Lock()
		defer hmc.logon.mu.Unlock()
	}
	if !hmc.logon.connected {
		log.Warnln("HMC Logoff. Attempting to logoff when connected")
		//return fmt.Errorf("Attempting to logoff when not connected")
		return nil
	}

	url := "https://" + hmc.hmcHostname + ":12443/rest/api/web/Logon"

	// Create request with context. Suppose ctx - context with timeout...
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("HMC Logoff request. %w", err)
	}

	// Set headers
	req.Header.Set("X-API-Session", hmc.logon.token)
	req.Header.Set("Host", hmc.hmcHostname+":12443")
	req.Header.Set("Accept", "*/*")

	// Execute request
	resp, err := hmc.client.Do(req)

	hmc.CloseIdleConnections()

	if err != nil {
		return fmt.Errorf("HMC Logoff do, %w", err)
	}
	defer resp.Body.Close()

	log.Debugf("Logoff %s status:%s", hmc.hmcName, resp.Status)
	//log.Debugf("Header:%v\n", resp.Header)

	hmc.logon.token = ""
	hmc.logon.connected = false

	body, _ := io.ReadAll(resp.Body)
	log.Debugf("Logoff Body: %s", body)

	if resp.StatusCode != 200 && resp.StatusCode != 202 && resp.StatusCode != 204 {
		return fmt.Errorf("Logoff failed error code: %s, url: %s", resp.Status, url)
	}
	return nil
}

func (hmc *HMC) reLogon(ctx context.Context, token string) (string, error) {

	hmc.logon.mu.Lock()
	defer hmc.logon.mu.Unlock()

	newToken := hmc.logon.token
	if newToken != token {
		log.Debugln("reLogon. New Token differ from old one. Smbdy already re-logoned.")
		return newToken, nil
	}
	_ = hmc.Logoff(ctx, false)
	err := hmc.Logon(ctx, false)
	return hmc.logon.token, err

}
func (hmc *HMC) Shutdown(ctx context.Context, wg *sync.WaitGroup) {

	defer wg.Done()

	log.Infoln("HMC shutting down..")
	if err := hmc.Logoff(ctx, true); err != nil {
		log.Warnf("HMC Logoff: %s", err)
	} else {
		log.Infoln("HMC Logoff OK")
	}
}

func (hmc *HMC) CloseIdleConnections() {
	//hmc.Logoff()
	log.Infoln("HMC closing idle connections.")
	hmc.client.CloseIdleConnections()
}

func (hmc *HMC) GetInfoByUrl(ctx context.Context, url string, headers map[string]string) ([]byte, error) {

	myname := "hmc.getInfoByUrl"

	log.Debugf("%s url=%s", myname, url)
	hmc.stats.url_requests++

	if !hmc.logon.connected {
		log.Infof("%s not connected. Trying to logon", myname)
		if err := hmc.Logon(ctx, true); err != nil {
			return []byte{}, fmt.Errorf("%s Not connected. Logon error: %w", myname, err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)

	if err != nil {
		return []byte{}, fmt.Errorf("%s %w", myname, err)
	}

	// Set headers
	token := hmc.logon.token // this token var is Inportant thinngs.
	// In case of authority error we will compare this token with hmc.logon.token,
	// may be smbdy already re-logoned while we processed the request
	req.Header.Set("X-API-Session", token)
	//req.Header.Set("Content-Type", "application/vnd.ibm.powervm.uom+xml; Type=ManagedSystem")
	req.Header.Set("Host", hmc.hmcHostname+":12443")
	req.Header.Set("Accept", "*/*")
	// Set custom headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	// Execute request
	resp, err := hmc.client.Do(req)
	if err != nil {
		return []byte{}, fmt.Errorf("%s %w", myname, err)
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
		// Not authorised - not logged on
		// try to logoff/logon once again
		log.Infof("%s not connected to HMC by response. Trying to Logoff/Logon.", myname)
		token, err := hmc.reLogon(ctx, token)
		if err == nil {
			// New token from new Logon and repeat the request
			req.Header.Set("X-API-Session", token)
			resp, err := hmc.client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				body, errBody := io.ReadAll(resp.Body)

				log.Debugf("%s status:%s, %d", myname, resp.Status, resp.StatusCode)

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
	// finally was not able to process the request
	return []byte{}, fmt.Errorf("%s response status: %s, url: %s", myname, resp.Status, url)
}
func (hmc *HMC) GetManagementConsole(ctx context.Context) ([]byte, error) {

	hmc.stats.mgmconsole_requests++

	consoleURL := "https://" + hmc.hmcHostname + ":12443/rest/api/uom/ManagementConsole"
	consoleHeader := map[string]string{}
	return hmc.GetInfoByUrl(ctx, consoleURL, consoleHeader)

	// Use read from file for development/debugging purposes in case real HMC is not reachable
	//return readFileSafely("./mgms.xml")
}
func (hmc *HMC) GemManagementConsoleData(ctx context.Context) (*ManagementConsole, error) {

	var mgmCons ManagementConsole
	myname := "GemManagementConsoleData"

	xmlData, err := hmc.GetManagementConsole(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s %s", myname, err)
	}
	err = xml.Unmarshal(xmlData, &mgmCons)
	if err != nil {
		return nil, fmt.Errorf("%s %s", myname, err)
	} else {
		return &mgmCons, nil
	}
}
func (hmc *HMC) GetMgmsQuick(ctx context.Context, mgmsUUID string) ([]byte, error) {

	hmc.stats.quick_mgms_requests++

	mgmsHeader := map[string]string{"Content-Type": "application/vnd.ibm.powervm.uom+xml; Type=ManagedSystem"}
	mgmsURL := "https://" + hmc.hmcHostname + ":12443/rest/api/uom/ManagedSystem/" + mgmsUUID + "/quick"
	return hmc.GetInfoByUrl(ctx, mgmsURL, mgmsHeader)

	// Use read from file for development/debugging purposes in case real HMC is not reachable
	//return readFileSafely("./mgms-quick.xml")
}

/*
func (hmc *HMC) getSystemLinks(mgmtConsole []byte) ([]string, error) {

	var feed ManagementConsole

	err := xml.Unmarshal(mgmtConsole, &feed)
	if err != nil {
		return []string{}, fmt.Errorf("failed to unmarshal XML: %w", err)
	} else {
		return []string{}, nil
	}
}
*/
