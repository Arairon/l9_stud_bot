package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/robfig/cron/v3"
	"stud.l9labs.ru/bot/modules/database"
	"stud.l9labs.ru/bot/modules/notify"
	"stud.l9labs.ru/bot/modules/site"
	"stud.l9labs.ru/bot/modules/ssauparser"
	"stud.l9labs.ru/bot/modules/tg"
)

var build string
var mainbot *tg.Bot

func main() {
	if err := tg.CheckEnv(); err != nil {
		log.Fatal(err)
	}
	ssauparser.HeadURL = os.Getenv("RASP_URL")
	log.SetOutput(io.MultiWriter(os.Stderr, database.InitLog("error")))
	help, err := os.ReadFile("help.txt")
	if err != nil {
		log.Fatal(err)
	}

	// bot.Debug = log.New(io.MultiWriter(os.Stderr, database.CreateLog("messages")), "", log.LstdFlags)
	mainbot, err = tg.InitBot(
		database.DB{
			User:   os.Getenv("MYSQL_USER"),
			Pass:   os.Getenv("MYSQL_PASS"),
			Schema: os.Getenv("MYSQL_DB"),
		},
		os.Getenv("TELEGRAM_APITOKEN"),
		build,
	)
	if err != nil {
		log.Fatal(err)
	}
	mainbot.Week, err = strconv.Atoi(os.Getenv("START_WEEK"))
	if err != nil {
		log.Fatal(err)
	}
	mainbot.TestUser, err = strconv.ParseInt(os.Getenv("TELEGRAM_TEST_USER"), 0, 64)
	if err != nil {
		log.Fatal(err)
	}
	mainbot.WkPath = os.Getenv("WK_PATH")
	mainbot.HelpTxt = string(help)
	c := cron.New()
	_, err = c.AddFunc("8/3 6-22 * * *", notifications)
	if err != nil {
		log.Fatal(err)
	}
	shedulePeriod, err := strconv.Atoi(os.Getenv("SHEDULES_CHECK_PERIOD"))
	if err != nil {
		log.Fatal(err)
	}
	_, err = c.AddFunc(fmt.Sprintf("@every %dm", shedulePeriod), sheduleCheck)
	if err != nil {
		log.Fatal(err)
	}
	c.Start()
	log.Println("Started")
	go sheduleCheck()
	go handleBot()

	router := mux.NewRouter()

	router.HandleFunc("/{fileNumber}.ics", site.GetICS).Methods("GET")
	server := &http.Server{
		Addr:         "localhost:8000",
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 1 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	log.Fatal(server.ListenAndServe())
}

func handleBot() {
	for update := range *mainbot.Updates {
		now := time.Now()
		_, err := mainbot.HandleUpdate(update, now)
		if err != nil {
			log.Println(err)
		}
	}
}

func notifications() {
	now := time.Now()
	now = now.Add(2 * time.Minute)
	//now := time.Date(2023, 9, 15, 17, 20, 0, 0, time.Local)
	log.Println(now)
	notes, err := notify.CheckNext(mainbot.DB, now)
	if err != nil {
		log.Println(err)
	}
	notify.Mailing(mainbot, notes)
	notify.FirstMailing(mainbot, now)
	notify.ClearTemp(mainbot, now)
}

func sheduleCheck() {
	now := time.Now()

	if now.Hour() > 8 && now.Hour() < 20 {
		notify.CheckShedules(mainbot, now)
	}
}
