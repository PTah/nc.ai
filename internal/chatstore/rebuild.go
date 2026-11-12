// Created by nc.ai 1.2.0 on new-admin-pc 2026-11-12 11:29:46
package chatstore

import (
	"encoding/json"
	"strings"

	"notcursor.ai/app/internal/llm"
)

// RebuiltItem — минимальный элемент UI-транскрипта: роль и текст. Инструменты
// и служебные шаги сюда не попадают: они невосстановимы по истории.
type RebuiltItem struct {
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

// ItemsFromHistory пересобирает UI-транскрипт из истории сессии ([]llm.Message).
//
// Зачем: транскрипт (ItemsJSON) и история — разные файлы-поля, и транскрипт может
// оказаться пустым или битым, хотя сама история жива (типичный случай — мягкая
// перезагрузка WebView посреди прогона). Тогда пользователь вместо своего диалога
// видел «история пуста». Восстановленный текст хуже исходного (нет вложений и
// карточек инструментов), но диалог не исчезает.
func ItemsFromHistory(history []llm.Message) string {
	items := make([]RebuiltItem, 0, len(history))
	for _, m := range history {
		text := strings.TrimSpace(m.Content)
		switch m.Role {
		case "user":
			if text != "" {
				items = append(items, RebuiltItem{Kind: "user", Content: text})
			}
		case "assistant":
			if text != "" {
				items = append(items, RebuiltItem{Kind: "assistant", Content: text})
			}
		}
	}
	if len(items) == 0 {
		return "[]"
	}
	out, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(out)
}
