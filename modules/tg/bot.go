package tg

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"git.l9labs.ru/anufriev.g.a/l9_stud_bot/modules/database"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"xorm.io/xorm"
)

type Bot struct {
	TG *tgbotapi.BotAPI
	DB *xorm.Engine
	// TG_user database.TgUser
	Week    int
	WkPath  string
	Debug   *log.Logger
	Updates *tgbotapi.UpdatesChannel
}

var env_keys = []string{
	"TELEGRAM_APITOKEN",
	"TELEGRAM_TEST_USER",
}

func CheckEnv() error {
	if err := godotenv.Load(); err != nil {
		log.Print("No .env file found")
	}
	for _, key := range env_keys {
		if _, exists := os.LookupEnv(key); !exists {
			return fmt.Errorf("lost env key: %s", key)
		}
	}
	return nil
}

// Полная инициализация бота со стороны Telegram и БД
func InitBot(db database.DB, token string) (*Bot, error) {
	var bot Bot
	engine, err := database.Connect(db)
	if err != nil {
		return nil, err
	}
	//defer engine.Close()
	bot.DB = engine

	bot.TG, err = tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}
	bot.TG.Debug = true
	//logger := log.New(io.MultiWriter(os.Stdout, database.CreateLog("tg")), "", log.LstdFlags)
	logger := log.New(database.CreateLog("tg"), "", log.LstdFlags)
	tgbotapi.SetLogger(logger)
	bot.GetUpdates()

	log.Printf("Authorized on account %s", bot.TG.Self.UserName)

	bot.Debug = log.New(io.MultiWriter(os.Stderr, database.CreateLog("messages")), "", log.LstdFlags)

	return &bot, nil
}

func (bot *Bot) GetUpdates() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.TG.GetUpdatesChan(u)
	bot.Updates = &updates
}

// Получение данных о пользователе из БД и создание нового при необходимости
func InitUser(db *xorm.Engine, user *tgbotapi.User) (*database.TgUser, error) {
	id := user.ID
	name := user.FirstName + " " + user.LastName

	var users []database.TgUser
	err := db.Find(&users, &database.TgUser{TgId: id})
	if err != nil {
		return nil, err
	}

	var tg_user database.TgUser
	if len(users) == 0 {
		l9id, err := database.GenerateID(db)
		if err != nil {
			return nil, err
		}

		user := database.User{
			L9Id: l9id,
		}

		tg_user = database.TgUser{
			L9Id:   l9id,
			Name:   name,
			TgId:   id,
			PosTag: database.NotStarted,
		}
		_, err = db.Insert(user, tg_user)
		if err != nil {
			return nil, err
		}
	} else {
		tg_user = users[0]
	}
	return &tg_user, nil
}

func (bot *Bot) HandleUpdate(update tgbotapi.Update) (tgbotapi.Message, error) {
	nilMsg := tgbotapi.Message{}
	if update.Message != nil {
		msg := update.Message
		user, err := InitUser(bot.DB, msg.From)
		if err != nil {
			return nilMsg, err
		}
		bot.Debug.Printf("Message [%d] <%s> %s", user.L9Id, user.Name, msg.Text)
		switch user.PosTag {
		case database.NotStarted:
			err = bot.Start(user)
		case database.Ready:
			return bot.Find(user, msg.Text)
		default:
			bot.Etc(user)
		}
		if err != nil {
			return nilMsg, err
		}
	}
	if update.CallbackQuery != nil {
		query := update.CallbackQuery
		user, err := InitUser(bot.DB, query.From)
		if err != nil {
			return nilMsg, err
		}
		bot.Debug.Printf("Callback [%d] <%s> %s", user.L9Id, user.Name, query.Data)
		switch user.PosTag {
		case database.NotStarted:
			err = bot.Start(user)
		case database.Ready:
			if !strings.Contains(query.Data, "sh") {
				err = bot.GetShedule(user, query)
			}
		default:
			bot.Etc(user)
		}
		if err != nil {
			return nilMsg, err
		}
		callback := tgbotapi.NewCallback(query.ID, query.Data)
		bot.TG.Request(callback)
	}
	return nilMsg, nil
}
