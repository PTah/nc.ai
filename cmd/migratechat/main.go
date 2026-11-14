// Edited by nc.ai 1.2.10 on macbook 2026-11-13 23:14:27
// Command migratechat копирует чат из одного проекта NotCursor в другой.
//
//	# какие чаты есть в проекте
//	go run ./cmd/migratechat -list -from "/путь/к/проекту"
//
//	# скопировать активный чат в другой проект
//	go run ./cmd/migratechat -from "/путь/источник" -to "/путь/цель"
//
//	# конкретный чат по id (достаточно префикса)
//	go run ./cmd/migratechat -from A -to B -session 6f2c1a
//
//	# перенести (удалить из источника) — только для закрытых чатов:
//	# приложение держит открытый чат в памяти и перезапишет удаление
//	go run ./cmd/migratechat -from A -to B -move
//
// Данные приложения по умолчанию берутся из os.UserConfigDir()/NotCursor
// (macOS: ~/Library/Application Support/NotCursor, Windows: %AppData%\NotCursor);
// каталог можно задать флагом -data.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"notcursor.ai/app/internal/chatstore"
)

func main() {
	var (
		dataDir  = flag.String("data", "", "каталог данных приложения (по умолчанию os.UserConfigDir()/NotCursor)")
		from     = flag.String("from", "", "проект-источник (путь)")
		to       = flag.String("to", "", "проект-получатель (путь)")
		session  = flag.String("session", "active", "какой чат: active | last | id (можно префикс)")
		title    = flag.String("title", "", "заголовок копии (по умолчанию как у источника)")
		move     = flag.Bool("move", false, "удалить чат из источника после копирования")
		listOnly = flag.Bool("list", false, "только показать чаты источника")
	)
	flag.Parse()

	dir := strings.TrimSpace(*dataDir)
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			fatalf("не удалось определить каталог данных: %v", err)
		}
		dir = filepath.Join(base, "NotCursor")
	}
	store, err := chatstore.New(dir)
	if err != nil {
		fatalf("chatstore: %v", err)
	}
	if strings.TrimSpace(*from) == "" {
		fatalf("нужен -from <путь проекта>")
	}
	bundle, err := store.List(*from)
	if err != nil {
		fatalf("чтение проекта %s: %v", *from, err)
	}
	if *listOnly {
		sessions := append([]chatstore.Session(nil), bundle.Sessions...)
		sort.Slice(sessions, func(i, j int) bool { return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt) })
		fmt.Printf("Проект: %s\n", *from)
		fmt.Printf("  чатов: %d, активный: %s\n", len(sessions), shortID(bundle.ActiveID))
		for _, s := range sessions {
			mark := " "
			if s.ID == bundle.ActiveID {
				mark = "*"
			}
			fmt.Printf("  %s %s  %-40s сообщений: %3d  обновлён: %s\n",
				mark, shortID(s.ID), truncate(s.Title, 40), len(s.History),
				s.UpdatedAt.Format("2006-01-02 15:04"))
		}
		return
	}
	if strings.TrimSpace(*to) == "" {
		fatalf("нужен -to <путь проекта>")
	}

	src, err := pickSession(bundle, *session)
	if err != nil {
		fatalf("%v", err)
	}
	sess, err := store.Get(*from, src.ID)
	if err != nil {
		fatalf("чтение чата %s: %v", src.ID, err)
	}

	newTitle := strings.TrimSpace(*title)
	if newTitle == "" {
		newTitle = sess.Title
	}
	dst, err := store.NewSession(*to, newTitle)
	if err != nil {
		fatalf("создание чата в %s: %v", *to, err)
	}
	dst.ItemsJSON = sess.ItemsJSON
	dst.History = sess.History
	dst.Model = sess.Model
	dst.ProviderID = sess.ProviderID
	copyUsage(dst, sess)
	if err := store.SaveSession(*to, dst); err != nil {
		fatalf("запись чата в %s: %v", *to, err)
	}

	fmt.Printf("Скопировано: %s → %s\n", *from, *to)
	fmt.Printf("  чат %s → %s «%s» (сообщений: %d)\n",
		shortID(sess.ID), shortID(dst.ID), dst.Title, len(dst.History))

	if *move {
		if err := store.DeleteSession(*from, sess.ID); err != nil {
			fatalf("копия создана, но удалить из источника не удалось: %v", err)
		}
		fmt.Printf("  из источника удалён (был активным в приложении? тогда перезапустите его)\n")
	}
	fmt.Printf("  проект %s назначен активным чатом — при открытии проекта откроется он\n", *to)
}

// pickSession выбирает чат по правилу: active / last / id (полный или префикс).
func pickSession(b *chatstore.ProjectBundle, want string) (chatstore.Session, error) {
	want = strings.TrimSpace(want)
	if len(b.Sessions) == 0 {
		return chatstore.Session{}, fmt.Errorf("в проекте нет чатов")
	}
	switch strings.ToLower(want) {
	case "", "active":
		for _, s := range b.Sessions {
			if s.ID == b.ActiveID {
				return s, nil
			}
		}
		return newest(b.Sessions), nil
	case "last", "newest":
		return newest(b.Sessions), nil
	}
	for _, s := range b.Sessions {
		if s.ID == want {
			return s, nil
		}
	}
	var hits []chatstore.Session
	for _, s := range b.Sessions {
		if strings.HasPrefix(s.ID, want) {
			hits = append(hits, s)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return chatstore.Session{}, fmt.Errorf("чат %q не найден", want)
	}
	return chatstore.Session{}, fmt.Errorf("префикс %q неоднозначен: %d совпадений", want, len(hits))
}

func newest(list []chatstore.Session) chatstore.Session {
	best := list[0]
	for _, s := range list[1:] {
		if s.UpdatedAt.After(best.UpdatedAt) {
			best = s
		}
	}
	return best
}

// copyUsage переносит счётчики расхода (все провайдерские бакеты).
func copyUsage(dst, src *chatstore.Session) {
	dst.CostUSD, dst.InputTokens, dst.OutputTokens = src.CostUSD, src.InputTokens, src.OutputTokens
	dst.DeepSeekCostUSD, dst.DeepSeekInputTokens, dst.DeepSeekOutputTokens = src.DeepSeekCostUSD, src.DeepSeekInputTokens, src.DeepSeekOutputTokens
	dst.DeepSeekCacheHitTokens, dst.DeepSeekCacheMissTokens = src.DeepSeekCacheHitTokens, src.DeepSeekCacheMissTokens
	dst.ZaiCostUSD, dst.ZaiInputTokens, dst.ZaiOutputTokens = src.ZaiCostUSD, src.ZaiInputTokens, src.ZaiOutputTokens
	dst.ZaiCacheHitTokens, dst.ZaiCacheMissTokens = src.ZaiCacheHitTokens, src.ZaiCacheMissTokens
	dst.OpenRouterCostUSD, dst.OpenRouterInputTokens, dst.OpenRouterOutputTokens = src.OpenRouterCostUSD, src.OpenRouterInputTokens, src.OpenRouterOutputTokens
	dst.OpenRouterCacheHitTokens, dst.OpenRouterCacheMissTokens = src.OpenRouterCacheHitTokens, src.OpenRouterCacheMissTokens
	dst.QwenCostUSD, dst.QwenInputTokens, dst.QwenOutputTokens = src.QwenCostUSD, src.QwenInputTokens, src.QwenOutputTokens
	dst.QwenCacheHitTokens, dst.QwenCacheMissTokens = src.QwenCacheHitTokens, src.QwenCacheMissTokens
	dst.KimiCostUSD, dst.KimiInputTokens, dst.KimiOutputTokens = src.KimiCostUSD, src.KimiInputTokens, src.KimiOutputTokens
	dst.KimiCacheHitTokens, dst.KimiCacheMissTokens = src.KimiCacheHitTokens, src.KimiCacheMissTokens
	dst.YandexCostUSD, dst.YandexInputTokens, dst.YandexOutputTokens = src.YandexCostUSD, src.YandexInputTokens, src.YandexOutputTokens
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func truncate(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
