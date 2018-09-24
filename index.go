package main

// kill $(cat pid)

import (
	"fmt"
	"net/http"
	"time"
	"errors"
	"encoding/json"
	"bytes"
	"github.com/sevlyar/go-daemon"
	"github.com/tkanos/gonfig"
	"github.com/jasonlvhit/gocron"
	"log"
	"os"
)

type Configuration struct {
	Name     string
	Server   string
	Email    string
	Password string
	Interval uint64
}

var SiteURL = ""
var username = ""
var password = ""
var ProbeName = "Unknown"

var minutesBetweenChecks uint64 = 5

var siteList = Sites{}

type Sites struct {
	Sites []Site `json:"sites"`
}

type Site struct {
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	UserID    string `json:"userID"`
}

type Response struct {
	SiteID       string  `json:"siteID"`
	CreatedAt    int64   `json:"createdAt"`
	Up           bool    `json:"up"`
	StatusCode   int     `json:"statusCode"`
	Status       string  `json:"status"`
	ResponseTime float64 `json:"responseTime"`
}

func main() {

	//update variables from config
	updateFromConfig()

	if !daemon.WasReborn() {
		//RUN IF PARENT
		//test auth
		fmt.Println("testing auth")
		err, _ := testAuth()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Println("Auth Successful")
	}

	cntxt := &daemon.Context{
		PidFileName: "pid",
		PidFilePerm: 0644,
		LogFileName: "log",
		LogFilePerm: 0640,
		WorkDir:     "./",
		Umask:       027,
		Args:        []string{"[Uptime Service Daemon]"},
	}

	if len(daemon.ActiveFlags()) > 0 {
		d, err := cntxt.Search()
		if err != nil {
			log.Fatalln("Unable send signal to the daemon:", err)
		}
		daemon.SendCommands(d)
		return
	}

	_, cntxterr := cntxt.Reborn()
	if cntxterr != nil {
		fmt.Println("Unable to run: ", cntxterr)
	}

	defer cntxt.Release()

	if daemon.WasReborn() {
		startSchedule()
	}

}

func updateFromConfig() {
	configuration := Configuration{}
	err := gonfig.GetConf("config.json", &configuration)
	if err != nil {
		panic(err)
	}

	ProbeName = configuration.Name
	username = configuration.Email
	password = configuration.Password
	SiteURL = configuration.Server
	minutesBetweenChecks = configuration.Interval

}

func startSchedule() {
	scheduler := gocron.NewScheduler()
	updateSitesList() //do this once so we have a list of sites
	checkSites()

	scheduler.Every(minutesBetweenChecks).Minutes().Do(updateSitesList)
	scheduler.Every(10).Minutes().Do(checkSites)
	<-scheduler.Start()
}

func requestGet(url string) (error, *http.Response) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(username, password)

	resp, err := client.Do(req)
	if err != nil {
		return err, &http.Response{}
	}

	if resp.StatusCode < 201 {
		return nil, resp
	} else {
		return errors.New("did not receive 200 code"), &http.Response{}
	}

}

func postResponse(url string, response Response) (error) {

	var jsonStr, _ = json.Marshal(response)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))
	req.SetBasicAuth(username, password)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return errors.New("got non 200 response code")
	}


	return nil
}

func updateSitesList() (error, []Site) {

	err, res := requestGet(SiteURL + "/api/sites")
	if err != nil {
		return err, []Site{}
	}

	defer res.Body.Close()

	siteList = Sites{}

	decodeError := json.NewDecoder(res.Body).Decode(&siteList)

	fmt.Println("received updated site list")

	return decodeError, siteList.Sites

}

func testAuth() (error, bool) {

	err, res := requestGet(SiteURL + "/api/sites")

	if err != nil {
		return err, false
	}

	if res.StatusCode < 201 {
		return nil, true
	} else {
		return errors.New("did not receive 200 code"), false
	}

}

func checkSites() {

	for _, site := range siteList.Sites {
		err, response := doRequest(site)
		if err != nil {
			fmt.Println(err)
		} else {

			postingError := postResponse(SiteURL+"/api/responses", response)

			if postingError != nil {
				fmt.Println("cannot access URL at this time")
			}
		}
	}

}

func doRequest(site Site) (err error, response Response) {

	start := time.Now()

	result, err := http.Get(site.URL)
	//defer result.Body.Close()

	if err != nil {
		fmt.Println("failed to reach", site.Name)
		return nil, Response{SiteID: site.ID, StatusCode: 0, Status: "Down", CreatedAt: time.Now().Unix(), ResponseTime: -1, Up: false}
	}

	elapsed := time.Since(start).Seconds() * 1e3

	fmt.Println(site.Name, "responseTime:", elapsed)

	return nil, Response{
		Up:           result.StatusCode == 200,
		Status:       result.Status,
		StatusCode:   result.StatusCode,
		CreatedAt:    time.Now().Unix(),
		ResponseTime: elapsed,
		SiteID:       site.ID,
	}
}

//
//func termHandler(sig os.Signal) error {
//	fmt.Println("terminating...")
//	scheduler.Clear()
//	return daemon.ErrStop
//}
