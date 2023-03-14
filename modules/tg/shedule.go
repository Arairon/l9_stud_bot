package tg

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"git.l9labs.ru/anufriev.g.a/l9_stud_bot/modules/database"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"xorm.io/xorm"
)

func (bot *Bot) GetSummary() {
	now := time.Now().Add(time.Hour * time.Duration(5))
	log.Println(now.Format("01-02-2006 15:04:05 -07"), now.Format("01-02-2006 15:04:05"))

	var lessons []database.Lesson
	var shedules []database.ShedulesInUser
	bot.DB.ID(bot.TG_user.L9Id).Find(&shedules)

	if len(shedules) == 0 {
		bot.Etc()
		return
	}

	var groups []string
	var teachers []string

	for _, sh := range shedules {
		if sh.IsTeacher {
			teachers = append(teachers, strconv.FormatInt(sh.SheduleId, 10))
		} else {
			groups = append(groups, strconv.FormatInt(sh.SheduleId, 10))
		}
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

	bot.DB.
		Where("end > ?", now.Format("2006-01-02 15:04:05")).
		And(condition).
		OrderBy("begin").
		Limit(16).
		Find(&lessons)

	log.Println(lessons)

	if len(lessons) != 0 {
		var firstPair, secondPair []database.Lesson
		pairs := GroupPairs(lessons)
		firstPair = pairs[0]
		secondPair = pairs[1]
		log.Println(firstPair, secondPair)

		var str string
		if pairs[0][0].Begin.Day() != time.Now().Day() {
			str = "❗️Сегодня пар нет\nБлижайшие занятия "
			if time.Until(firstPair[0].Begin).Hours() < 48 {
				str += "завтра\n"
			} else {
				str += fmt.Sprintf("%s\n\n", firstPair[0].Begin.Format("02.01"))
			}
			day, _ := bot.GetDayShedule(pairs)
			str += day
		} else {
			str = "Сводка на сегодня\n\n"
			day, _ := bot.GetDayShedule(pairs)
			str += day
			/*
				firstPairStr, _ := PairToStr(firstPair, &bot.DB)
				str += firstPairStr

				if len(secondPair) != 0 && firstPair[0].Begin.Day() == secondPair[0].Begin.Day() {
					secondPairStr, _ := PairToStr(secondPair, &bot.DB)
					str += "\n--\n" + secondPairStr
				}
			*/
		}

		msg := tgbotapi.NewMessage(bot.TG_user.TgId, str)
		bot.TG.Send(msg)
	}

}

func (bot *Bot) GetDayShedule(lessons [][]database.Lesson) (string, error) {
	var str string
	day := lessons[0][0].Begin.Day()
	for _, pair := range lessons {
		if pair[0].Begin.Day() == day {
			line, err := PairToStr(pair, &bot.DB)
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

func PairToStr(pair []database.Lesson, db *xorm.Engine) (string, error) {
	var str string
	beginStr := pair[0].Begin.Format("15:04")
	endStr := pair[0].End.Format("15:04")
	str = fmt.Sprintf("📆 %s - %s\n", beginStr, endStr)

	for i, sublesson := range pair {
		var type_emoji string
		switch sublesson.Type {
		case "lect":
			type_emoji = "📗"
		case "pract":
			type_emoji = "📕"
		case "lab":
			type_emoji = "📘"
		case "other":
			type_emoji = "📙"
		default:
			type_emoji = "📙"
		}
		str += fmt.Sprintf("%s%s\n", type_emoji, sublesson.Name)
		if sublesson.Place != "" {
			str += fmt.Sprintf("🧭 %s\n", sublesson.Place)
		}
		if sublesson.TeacherId != 0 {
			var t database.Teacher
			_, err := db.ID(sublesson.TeacherId).Get(&t)
			if err != nil {
				return "", err
			}
			name := fmt.Sprintf("%s %s.%s.", t.LastName, t.FirstName[0:2], t.MidName[0:2])
			str += fmt.Sprintf("👤 %s\n", name)
		}
		if sublesson.SubGroup != "" {
			str += fmt.Sprintf("👥 %s\n", sublesson.SubGroup)
		}
		if sublesson.Comment != "" {
			str += fmt.Sprintf("💬 %s\n", sublesson.Comment)
		}
		if i != len(pair)-1 {
			str += "+\n"
		}
	}

	str += "------------------------------------------\n"
	return str, nil
}
