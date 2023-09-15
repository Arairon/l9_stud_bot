package tg

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"git.l9labs.ru/anufriev.g.a/l9_stud_bot/modules/database"
	"git.l9labs.ru/anufriev.g.a/l9_stud_bot/modules/ssau_parser"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	td "github.com/mergestat/timediff"
	"xorm.io/xorm"
)

func (bot *Bot) GetPersonal(now time.Time, user *database.TgUser, editMsg ...tgbotapi.Message) (tgbotapi.Message, error) {
	shedule := database.ShedulesInUser{L9Id: user.L9Id}
	exists, err := bot.DB.Get(&shedule)
	if err != nil {
		return nilMsg, err
	}

	if !exists {
		user.PosTag = database.Add
		if _, err := bot.DB.ID(user.L9Id).Update(user); err != nil {
			return tgbotapi.Message{}, err
		}

		return bot.SendMsg(
			user,
			"У тебя пока никакого расписания не подключено\n"+
				"Введи <b>номер группы</b>\n"+
				"(в формате 2305 или 2305-240502D)",
			tgbotapi.ReplyKeyboardRemove{RemoveKeyboard: true},
		)
	} else {
		return nilMsg, bot.GetWeekSummary(now, user, shedule, -1, true, "")
		//return bot.GetSummary(now, user, shedules, true, editMsg...)
	}
}

// Получить краткую сводку
//
// Если isPersonal == false, то обязательно заполнение объекта shedule
//
// При isPersonal == true, объект shedule игнорируется
func (bot *Bot) GetShortSummary(
	now time.Time,
	user *database.TgUser,
	shedule database.ShedulesInUser,
	isPersonal bool,
	editMsg ...tgbotapi.Message,
) (
	tgbotapi.Message,
	error,
) {
	if err := bot.ActShedule(isPersonal, user, &shedule); err != nil {
		return nilMsg, err
	}
	lessons, err := bot.GetLessons(shedule, now, 32)
	if err != nil {
		return nilMsg, err
	}
	if len(lessons) != 0 {
		var firstPair, secondPair []database.Lesson
		pairs := GroupPairs(lessons)
		firstPair = pairs[0]
		str := "📝Краткая сводка:\n\n"
		if pairs[0][0].Begin.Day() != now.Day() {
			str += "❗️Сегодня пар нет\nБлижайшие занятия "
			str += td.TimeDiff(
				firstPair[0].Begin,
				td.WithLocale("ru_RU"),
				td.WithStartTime(now),
			)
			if firstPair[0].Begin.Sub(now).Hours() > 36 {
				str += fmt.Sprintf(
					", <b>%d %s</b>",
					firstPair[0].Begin.Day(),
					Month[firstPair[0].Begin.Month()-1],
				)
			}
			str += "\n\n"
			day, err := bot.StrDayShedule(pairs, shedule.IsGroup)
			if err != nil {
				return nilMsg, err
			}
			str += day
		} else {
			if firstPair[0].Begin.Before(now) {
				str += "Сейчас:\n\n"
			} else {
				dt := td.TimeDiff(
					firstPair[0].Begin,
					td.WithLocale("ru_RU"),
					td.WithStartTime(now),
				)
				str += fmt.Sprintf("Ближайшая пара %s:\n\n", dt)
			}
			firstStr, err := PairToStr(firstPair, bot.DB, shedule.IsGroup)
			if err != nil {
				return nilMsg, err
			}
			str += firstStr
			if len(pairs) > 1 {
				secondPair = pairs[1]
				if firstPair[0].Begin.Day() == secondPair[0].Begin.Day() {
					str += "\nПосле неё:\n\n"
					secondStr, err := PairToStr(secondPair, bot.DB, shedule.IsGroup)
					if err != nil {
						return nilMsg, err
					}
					str += secondStr
				} else {
					str += "\nБольше ничего сегодня нет"
				}
			} else {
				str += "\nБольше ничего сегодня нет"
			}

		}
		markup := SummaryKeyboard(
			Near,
			shedule,
			isPersonal,
			0,
		)
		return bot.EditOrSend(user.TgId, str, "", markup, editMsg...)

	} else {
		return bot.EditOrSend(
			user.TgId,
			"Ой! Занятий не обнаружено ):",
			"",
			tgbotapi.InlineKeyboardMarkup{},
			editMsg...)
	}
}

// Актуализация запроса на расписание для персональных расписаний
func (bot *Bot) ActShedule(isPersonal bool, user *database.TgUser, shedule *database.ShedulesInUser) error {
	if isPersonal {
		if _, err := bot.DB.Where("L9Id = ?", user.L9Id).Get(shedule); err != nil {
			return err
		}
	}
	return nil
}

// Получить расписание на день
//
// Если isPersonal == false, то обязательно заполнение объекта shedule
//
// При isPersonal == true, объект shedule игнорируется
func (bot *Bot) GetDaySummary(
	now time.Time,
	user *database.TgUser,
	shedule database.ShedulesInUser,
	dt int,
	isPersonal bool,
	editMsg ...tgbotapi.Message,
) (
	tgbotapi.Message,
	error,
) {
	day := time.Date(now.Year(), now.Month(), now.Day()+dt, 0, 0, 0, 0, now.Location())
	if err := bot.ActShedule(isPersonal, user, &shedule); err != nil {
		return nilMsg, err
	}
	lessons, err := bot.GetLessons(shedule, day, 32)
	if err != nil {
		return nilMsg, err
	}
	if len(lessons) != 0 {
		pairs := GroupPairs(lessons)
		var str string
		firstPair := pairs[0][0].Begin
		dayStr := DayStr(day)

		markup := SummaryKeyboard(Day, shedule, isPersonal, dt)

		if firstPair.Day() != day.Day() {
			str = fmt.Sprintf("В %s, занятий нет", dayStr)
			return bot.EditOrSend(user.TgId, str, "", markup, editMsg...)
		}
		str = fmt.Sprintf("Расписание на %s\n\n", dayStr)

		// TODO: придумать скачки для пустых дней
		//dt += int(firstPair.Sub(day).Hours()) / 24
		day, err := bot.StrDayShedule(pairs, shedule.IsGroup)
		if err != nil {
			return nilMsg, err
		}
		str += day
		return bot.EditOrSend(user.TgId, str, "", markup, editMsg...)
	} else {
		return bot.SendMsg(user, "Ой! Пар не обнаружено ):", GeneralKeyboard(true))
	}

}

// Строка даты формата "среду, 1 января"
func DayStr(day time.Time) string {
	dayStr := fmt.Sprintf(
		"%s, <b>%d %s</b>",
		weekdays[int(day.Weekday())],
		day.Day(),
		Month[day.Month()-1],
	)
	return dayStr
}

// Получить список ближайших занятий (для краткой сводки или расписания на день)
func (bot *Bot) GetLessons(shedule database.ShedulesInUser, now time.Time, limit int) ([]database.Lesson, error) {

	condition := CreateCondition(shedule)

	var lessons []database.Lesson
	err := bot.DB.
		Where("end > ?", now.Format("2006-01-02 15:04:05")).
		And(condition).
		OrderBy("begin").
		Limit(limit).
		Find(&lessons)

	return lessons, err
}

// Загрузка расписания из ssau.ru/rasp
func (bot *Bot) LoadShedule(shedule ssau_parser.WeekShedule, now time.Time) (
	[]database.Lesson,
	[]database.Lesson,
	error,
) {
	sh := ssau_parser.WeekShedule{
		SheduleId: shedule.SheduleId,
		IsGroup:   shedule.IsGroup,
	}
	var add, del []database.Lesson
	for week := 1; week < 21; week++ {
		sh.Week = week
		if err := sh.DownloadById(true); err != nil {
			if strings.Contains(err.Error(), "404") {
				break
			}
			return nil, nil, err
		}
		a, d, err := ssau_parser.UpdateSchedule(bot.DB, sh)
		if err != nil {
			return nil, nil, err
		}
		add = append(add, a...)
		del = append(del, d...)
	}
	// Обновляем время обновления
	if len(add) > 0 || len(del) > 0 {
		if sh.IsGroup {
			gr := database.Group{GroupId: sh.SheduleId}
			if _, err := bot.DB.Get(&gr); err != nil {
				return nil, nil, err
			}
			gr.LastUpd = now
			if _, err := bot.DB.ID(gr.GroupId).Update(gr); err != nil {
				return nil, nil, err
			}
		} else {
			t := database.Teacher{TeacherId: sh.SheduleId}
			if _, err := bot.DB.Get(&t); err != nil {
				return nil, nil, err
			}
			t.LastUpd = now
			if _, err := bot.DB.ID(t.TeacherId).Update(t); err != nil {
				return nil, nil, err
			}
		}
	}
	return add, del, nil
}

// Создать условие поиска группы/преподавателя
func CreateCondition(shedule database.ShedulesInUser) string {
	var groups []string
	var teachers []string

	if !shedule.IsGroup {
		teachers = append(teachers, strconv.FormatInt(shedule.SheduleId, 10))
	} else {
		groups = append(groups, strconv.FormatInt(shedule.SheduleId, 10))
	}

	var condition, teachers_str, groups_str string
	if len(groups) > 0 {
		groups_str = strings.Join(groups, ",")
		condition = "groupId in (" + groups_str + ") "
	}
	if len(teachers) > 0 {
		if len(condition) > 0 {
			condition += " or "
		}
		teachers_str += strings.Join(teachers, ",")
		condition += "teacherId in (" + teachers_str + ") "
	}
	return condition
}

// Группировка занятий по парам
func GroupPairs(lessons []database.Lesson) [][]database.Lesson {
	var shedule [][]database.Lesson
	var pair []database.Lesson

	l_idx := 0

	for l_idx < len(lessons) {
		day := lessons[l_idx].Begin
		for l_idx < len(lessons) && lessons[l_idx].Begin == day {
			pair = append(pair, lessons[l_idx])
			l_idx++
		}
		shedule = append(shedule, pair)
		pair = []database.Lesson{}
	}
	return shedule
}

var Icons = map[string]string{
	"lect":   "📗",
	"pract":  "📕",
	"lab":    "📘",
	"other":  "📙",
	"mil":    "🫡",
	"window": "🏝",
	"exam":   "💀",
	"cons":   "🗨",
	"kurs":   "🤯",
}

var Comm = map[string]string{
	"lect":   "Лекция",
	"pract":  "Практика",
	"lab":    "Лаба",
	"other":  "Прочее",
	"mil":    "",
	"window": "",
	"exam":   "Экзамен",
	"cons":   "Консультация",
	"kurs":   "Курсовая",
}

// Конвертация занятий с текст
func PairToStr(pair []database.Lesson, db *xorm.Engine, isGroup bool) (string, error) {
	var str string
	beginStr := pair[0].Begin.Format("15:04")
	var endStr string
	if pair[0].Type == "mil" {
		endStr = "∞"
	} else {
		endStr = pair[0].End.Format("15:04")
	}
	str = fmt.Sprintf("📆 %s - %s\n", beginStr, endStr)

	var groups []database.Lesson
	if !isGroup {
		groups = pair[:]
		pair = pair[:1]
	}

	for i, sublesson := range pair {
		type_emoji := Icons[sublesson.Type] + " " + Comm[sublesson.Type]
		str += fmt.Sprintf("%s %s\n", type_emoji, sublesson.Name)
		if sublesson.Place != "" {
			str += fmt.Sprintf("🧭 %s\n", sublesson.Place)
		}
		if !isGroup {
			break
		}
		if sublesson.TeacherId != 0 {
			var t database.Teacher
			_, err := db.ID(sublesson.TeacherId).Get(&t)
			if err != nil {
				return "", err
			}
			str += fmt.Sprintf("👤 %s %s\n", t.FirstName, t.ShortName)
		}
		if sublesson.SubGroup != 0 {
			str += fmt.Sprintf("👥 Подгруппа: %d\n", sublesson.SubGroup)
		}
		if sublesson.Comment != "" {
			str += fmt.Sprintf("💬 %s\n", sublesson.Comment)
		}
		if i != len(pair)-1 {
			str += "+\n"
		}
	}

	if !isGroup {
		for _, gr := range groups {
			var t database.Group
			_, err := db.ID(gr.GroupId).Get(&t)
			if err != nil {
				return "", err
			}
			str += fmt.Sprintf("👥 %s\n", t.GroupName)
			if gr.SubGroup != 0 {
				str += fmt.Sprintf("👥 Подгруппа: %d\n", gr.SubGroup)
			}
		}
		if pair[0].Comment != "" {
			str += fmt.Sprintf("💬 %s\n", pair[0].Comment)
		}
	}

	str += "------------------------------------------\n"
	return str, nil
}

// Текст расписания на день
func (bot *Bot) StrDayShedule(lessons [][]database.Lesson, isGroup bool) (string, error) {
	var str string
	day := lessons[0][0].Begin.Day()
	for _, pair := range lessons {
		if pair[0].Begin.Day() == day {
			line, err := PairToStr(pair, bot.DB, isGroup)
			if err != nil {
				return "", err
			}
			str += line
		} else {
			break
		}
	}
	return str, nil
}
