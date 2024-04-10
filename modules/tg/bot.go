package tg

import (
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"stud.l9labs.ru/bot/modules/api"
	"stud.l9labs.ru/bot/modules/database"
	"xorm.io/xorm"
)

type Bot struct {
	Name      string
	TG        *tgbotapi.BotAPI
	DB        *xorm.Engine
	TestUser  int64
	HelpTxt   string
	StartTxt  string
	Week      int
	WkPath    string
	Debug     *log.Logger
	Updates   *tgbotapi.UpdatesChannel
	Messages  int64
	Callbacks int64
	Build     string
	IsDebug   bool
}

const (
	Group = "group"
)

var envKeys = []string{
	"TELEGRAM_APITOKEN",
	"TELEGRAM_TEST_USER",
	"WK_PATH",
	"MYSQL_USER",
	"MYSQL_PASS",
	"MYSQL_DB",
	"START_WEEK",
	"RASP_URL",
	"NOTIFY_PERIOD",
	"SHEDULES_CHECK_PERIOD",
}

func CheckEnv() error {
	if err := godotenv.Load(); err != nil {
		log.Print("No .env file found")
	}
	for _, key := range envKeys {
		if _, exists := os.LookupEnv(key); !exists {
			return fmt.Errorf("lost env key: %s", key)
		}
	}

	return nil
}

// Полная инициализация бота со стороны Telegram и БД
func InitBot(db database.DB, token string, build string) (*Bot, error) {
	var bot Bot
	bot.Build = build
	bot.IsDebug = os.Getenv("DEBUG") == "1"
	engine, err := database.Connect(db, database.InitLog("sql"))
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
	logger := log.New(database.InitLog("tg"), "", log.LstdFlags)
	err = tgbotapi.SetLogger(logger)
	if err != nil {
		return nil, err
	}
	bot.GetUpdates()

	bot.Name = bot.TG.Self.UserName
	log.Printf("Authorized on account %s", bot.Name)
	bot.Debug = log.New(io.MultiWriter(os.Stderr, database.InitLog("messages")), "", log.LstdFlags)

	return &bot, nil
}

func (bot *Bot) GetUpdates() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.TG.GetUpdatesChan(u)
	bot.Updates = &updates
}

func (bot *Bot) SendMsg(user *database.TgUser, text string, markup interface{}) (tgbotapi.Message, error) {
	msg := tgbotapi.NewMessage(user.TgId, text)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = markup

	return bot.TG.Send(msg)
}

// Получение данных о пользователе из БД и создание нового при необходимости
func InitUser(db *xorm.Engine, user *tgbotapi.User) (*database.TgUser, error) {
	id := user.ID
	//name := user.FirstName + " " + user.LastName

	var users []database.TgUser
	err := db.Find(&users, &database.TgUser{TgId: id})
	if err != nil {
		return nil, err
	}

	var tgUser database.TgUser
	if len(users) == 0 {
		l9id, err := database.GenerateID(db, &database.User{})
		if err != nil {
			return nil, err
		}

		user := database.User{
			L9Id: l9id,
		}

		tgUser = database.TgUser{
			L9Id: l9id,
			//Name:   name,
			TgId:   id,
			PosTag: database.NotStarted,
		}
		_, err = db.Insert(user, tgUser)
		if err != nil {
			return nil, err
		}
	} else {
		tgUser = users[0]
	}

	return &tgUser, nil
}

func (bot *Bot) DeleteUser(user database.TgUser) error {
	if _, err := bot.DB.Delete(&user); err != nil {
		return err
	}
	if _, err := bot.DB.Delete(&database.ShedulesInUser{L9Id: user.L9Id}); err != nil {
		return err
	}
	if _, err := bot.DB.Delete(&database.User{L9Id: user.L9Id}); err != nil {
		return err
	}
	if _, err := bot.DB.Delete(&database.File{TgId: user.TgId}); err != nil {
		return err
	}
	if _, err := bot.DB.Delete(&database.ICalendar{L9ID: user.L9Id}); err != nil {
		return err
	}

	return nil
}

func (bot *Bot) HandleUpdate(update tgbotapi.Update, now ...time.Time) (tgbotapi.Message, error) {
	if len(now) == 0 {
		now = append(now, time.Now())
	}
	if update.Message != nil {
		return bot.HandleMessage(update.Message, now[0])
	}
	if update.CallbackQuery != nil {
		return bot.HandleCallback(update.CallbackQuery, now[0])
	}
	if update.InlineQuery != nil {
		return bot.HandleInlineQuery(update)
	}
	if update.MyChatMember != nil {
		return bot.ChatActions(update)
	}

	return nilMsg, nil
}

func (bot *Bot) HandleMessage(msg *tgbotapi.Message, now time.Time) (tgbotapi.Message, error) {
	if bot.IsDebug && msg.From.ID != bot.TestUser {
		return nilMsg, nil
	}
	// Игнорируем "сообщения" о входе в чат
	if len(msg.NewChatMembers) != 0 || msg.LeftChatMember != nil {
		return nilMsg, nil
	}
	if msg.Chat.IsGroup() &&
		len(msg.Entities) != 0 &&
		msg.Entities[0].Type == "bot_command" {

		return bot.HandleGroup(msg, now)
	}
	user, err := InitUser(bot.DB, msg.From)
	if err != nil {
		return nilMsg, err
	}

	bot.Debug.Printf("Message  [%10d:%10d] %s", user.L9Id, user.TgId, msg.Text)
	bot.Messages++
	if msg.Text == "Моё расписание" || msg.Text == "Настройки" {
		return bot.SendMsg(
			user,
			"Кнопки больше не работают, используй команды /schedule и /options",
			tgbotapi.ReplyKeyboardRemove{RemoveKeyboard: true},
		)
	}
	if strings.Contains(msg.Text, "/help") {
		return bot.SendMsg(user, bot.HelpTxt, nilKey)
	}
	if strings.Contains(msg.Text, "/start") && user.PosTag != database.NotStarted {
		if err := bot.DeleteUser(*user); err != nil {
			return nilMsg, err
		}
		if _, err = bot.SendMsg(
			user,
			"Весь прогресс сброшен\nДобро пожаловать снова (:",
			tgbotapi.ReplyKeyboardRemove{RemoveKeyboard: true},
		); err != nil {
			return nilMsg, err
		}
		user, err = InitUser(bot.DB, msg.From)
		if err != nil {
			return nilMsg, err
		}
	}
	switch user.PosTag {
	case database.NotStarted:
		return bot.Start(user)
	case database.Ready:
		if KeywordContains(msg.Text, AdminKey) && user.TgId == bot.TestUser {
			return bot.AdminHandle(msg)
		} else if strings.Contains(msg.Text, "/schedule") {
			sch := database.Schedule{
				IsPersonal: true,
				TgUser:     user,
			}

			return bot.GetPersonal(now, sch)
		} else if strings.Contains(msg.Text, "/options") {
			return bot.GetOptions(user)
		} else if strings.Contains(msg.Text, "/keyboard") {
			return bot.SendMsg(
				user,
				"Кнопки больше не работают, используй команды /schedule и /options",
				nil,
			)
		} else if strings.Contains(msg.Text, "/session") {
			return bot.SendMsg(
				user,
				"На данный момент информации о сессии пока нет",
				//"Расписание сессии теперь можно посмотреть прямо в карточке с расписанием!",
				nil,
			)
		} else if KeywordContains(msg.Text, []string{"/group", "/staff"}) {
			return bot.GetSheduleFromCmd(now, user, msg.Text)
		} else if strings.Contains(msg.Text, "/") {
			return bot.SendMsg(
				user,
				"Неопознанная команда\nВсе доступные команды можно посмотреть в разделе Меню\n👇",
				nil,
			)
		}

		return bot.Find(now, user, msg.Text)
	case database.Set:
		return bot.SetFirstTime(msg, user)
	case database.Delete:
		return bot.DeleteGroup(user, msg.Text)

	default:
		return bot.Etc(user)
	}
}

func (bot *Bot) HandleCallback(query *tgbotapi.CallbackQuery, now time.Time) (tgbotapi.Message, error) {
	user, err := InitUser(bot.DB, query.From)
	if err != nil {
		return nilMsg, err
	}
	bot.Debug.Printf("Callback [%10d:%10d] %s", user.L9Id, user.TgId, query.Data)
	bot.Callbacks++
	if query.Data == "cancel" {
		return nilMsg, bot.Cancel(user, query)
	}
	if user.PosTag == database.NotStarted {
		return bot.Start(user)
	} else if user.PosTag == database.Ready {
		if strings.Contains(query.Data, SummaryPrefix) {
			err = bot.HandleSummary(user, query, now)
		} else if strings.Contains(query.Data, "opt") {
			err = bot.HandleOptions(user, query)
		} else if strings.Contains(query.Data, "note") {
			err = bot.AddNote(query, user)
		} else {
			err = bot.GetShedule(user, query, now)
		}
	} else {
		return bot.Etc(user)
	}

	// Обработка ошибок
	if err != nil {
		if strings.Contains(err.Error(), "message is not modified") {
			callback := tgbotapi.NewCallback(query.ID, "Ничего не изменилось")
			_, err = bot.TG.Request(callback)
			if err != nil {
				return nilMsg, err
			}
			bot.Debug.Println("Message is not modified")

			return nilMsg, nil
		} else if strings.Contains(err.Error(), "no lessons") {
			callback := tgbotapi.NewCallback(query.ID, "Тут занятий уже нет. Возможно, их нет и на сайте")
			_, err = bot.TG.Request(callback)
			if err != nil {
				return nilMsg, err
			}
			bot.Debug.Println(err)
		}

		return nilMsg, err
	}

	return nilMsg, nil
}

// Выбор занятия для добавления заметки
func (bot *Bot) AddNote(query *tgbotapi.CallbackQuery, user *database.TgUser) error {
	id, err := strconv.ParseInt(query.Data[5:], 10, 64)
	if err != nil {
		return err
	}

	lesson, err := api.GetLesson(bot.DB, id)
	if err != nil {
		return err
	}

	userInfo := database.ShedulesInUser{
		L9Id: user.L9Id,
	}
	if _, err = bot.DB.Get(&userInfo); err != nil {
		return err
	}

	if !(userInfo.IsAdmin && userInfo.SheduleId == lesson.GroupId) {
		_, err = bot.SendMsg(
			user,
			"У вас нет привилегий для добавления заметок в расписании\n"+
				"Обратитесь к старосте группы или другим привилегированным лицам",
			nil,
		)

		return err
	}

	// Следующая пара того же типа
	nextLesson := database.Lesson{
		Type:      lesson.Type,
		Name:      lesson.Name,
		GroupId:   lesson.GroupId,
		TeacherId: lesson.TeacherId,
	}

	if _, err = bot.DB.
		Where("DATE(Begin) > ?", lesson.Begin.Format("2006-01-02")).
		Asc("Begin").
		Get(&nextLesson); err != nil {
		return err
	}

	// Следующая пара с тем же преподавателем (вне зависимости от типа и названия)
	nextStaff := database.Lesson{
		GroupId:   lesson.GroupId,
		TeacherId: lesson.TeacherId,
	}
	if _, err = bot.DB.
		Where("DATE(Begin) > ?", lesson.Begin.Format("2006-01-02")).
		Asc("Begin").
		Get(&nextStaff); err != nil {
		return err
	}

	var markup [][]tgbotapi.InlineKeyboardButton
	if nextLesson.LessonId != 0 {
		str := fmt.Sprintf(
			"Следующая %s%s (%s)",
			Icons[nextLesson.Type],
			Comm[nextLesson.Type],
			nextLesson.Begin.Format("02.01 15:04"),
		)
		q := fmt.Sprintf("add_%d", nextLesson.LessonId)
		markup = append(
			markup,
			[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(str, q)},
		)
	}

	if nextStaff.LessonId != 0 {
		staff, err := api.GetStaff(bot.DB, nextStaff.TeacherId)
		if err != nil {
			return err
		}
		str := fmt.Sprintf(
			"Следующее с %s %s (%s)",
			staff.FirstName,
			staff.ShortName,
			nextStaff.Begin.Format("02.01 15:04"),
		)
		q := fmt.Sprintf("add_%d", nextStaff.LessonId)
		markup = append(
			markup,
			[]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardButtonData(str, q)},
		)
	}

	_, err = bot.SendMsg(
		user,
		"Выберите занятие для заметки\n\nЕсли нужного занятия нет в списке, то его можно найти в расписании на день",
		tgbotapi.InlineKeyboardMarkup{InlineKeyboard: markup},
	)

	return err
}

func (bot *Bot) CheckBlocked(err error, user database.TgUser) {
	if strings.Contains(err.Error(), "blocked by the user") {
		if err := bot.DeleteUser(user); err != nil {
			log.Println(err)
		}

		return
	}
	log.Println(err)
}
