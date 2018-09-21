package main

// kill $(cat pid)

import (
	"golang.org/x/crypto/ssh/terminal"
	"github.com/fatih/color"
	"fmt"
	"net/http"
	"time"
	"os"
	"bufio"
	"syscall"
	"strings"
	"errors"
	"encoding/json"
	"bytes"
	"strconv"
	"github.com/jasonlvhit/gocron"
	"github.com/sevlyar/go-daemon"
	"flag"
	"log"
)

var SiteURL = "http://127.0.0.1:3000"

var username = ""
var password = ""

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

var (
	signal = flag.String("s", "", `send signal to the daemon
        stop — shutdown`)
)

var scheduler = gocron.NewScheduler()

func main() {

	flag.Parse()
	daemon.AddCommand(daemon.StringFlag(signal, "stop"), syscall.SIGTERM, termHandler)

	termHandler(nil)

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

	_, err := cntxt.Reborn()
	if err != nil {
		fmt.Println("Unable to run: ", err)
	}

	defer cntxt.Release()

	if daemon.WasReborn() {
		//CHILD
		fmt.Println("username/password:", username, password)
		fmt.Println(color.GreenString("Starting Daemon"))
		startSchedule()
	} else {
		//PARENT
		authenticate()
		requestUpdateInterval()
	}
}

func requestUpdateInterval() {
	fmt.Print("How ofter do you want to check sites? (minutes):")
	reader := bufio.NewReader(os.Stdin)
	interval, _ := reader.ReadString('\n')
	intervalNumber, intParseError := strconv.ParseUint(interval, 10, 64)
	if intParseError == nil {
		minutesBetweenChecks = intervalNumber
	}

	fmt.Println(color.BlueString("Checking every"), color.RedString(fmt.Sprint(minutesBetweenChecks)), color.BlueString("minutes"))
}

func startSchedule() {

	updateSitesList() //TODO do this once so we have a list of sites

	//scheduler := gocron.NewScheduler()
	scheduler.Every(minutesBetweenChecks).Minutes().Do(updateSitesList)
	scheduler.Every(10).Minutes().Do(checkSites)
	<-scheduler.Start()
}

func authenticate() {
	username, password = credentials()
	err, ok := testAuth()

	if err != nil {
		fmt.Println(err)
	}

	if !ok {
		fmt.Println("username and password did not work, please try again");
		authenticate()
	} else {
		fmt.Println(color.GreenString("Signed in"))
	}
}
func credentials() (string, string) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter Email: ")
	username, _ := reader.ReadString('\n')

	fmt.Print("Enter Password: ")
	bytePassword, _ := terminal.ReadPassword(int(syscall.Stdin))
	//if err == nil {
	//	fmt.Println("\nPassword typed: " + string(bytePassword))
	//}
	password := string(bytePassword)

	return strings.TrimSpace(username), strings.TrimSpace(password)
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
	//fmt.Println("json string", jsonStr)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))
	req.SetBasicAuth(username, password)
	//req.Header.Set("X-Custom-Header", "myvalue")
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

	//fmt.Println("response Status:", resp.Status)
	//fmt.Println("response Headers:", resp.Header)
	//body, _ := ioutil.ReadAll(resp.Body)
	//fmt.Println("response Body:", string(body))

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

		//TODO still report back as it just means the site is down
		fmt.Println("failed to reach", site.Name)
		return nil, Response{SiteID: site.ID, StatusCode: 0, Status: "Down", CreatedAt: time.Now().Unix(), ResponseTime: -1, Up: false}
		//return err, Response{}
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

func termHandler(sig os.Signal) error {
	fmt.Println("terminating...")
	scheduler.Clear()
	return daemon.ErrStop
}
