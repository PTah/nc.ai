// Created by nc.ai 0.9.4 on new-admin-pc 2026-10-30 09:04:59
package tools

import (
	"crypto/rand"
	"encoding/hex"
)

// wrapUntrusted оборачивает внешний контент (веб-страницу, результаты поиска) в
// случайный <data-…> тег. Системное правило агента говорит, что текст внутри
// таких тегов — данные, а не инструкции. Случайное имя на каждый вызов не даёт
// атакующему закрыть тег заранее заготовленным </…> и «выйти» из данных.
func wrapUntrusted(s string) string {
	if s == "" {
		return s
	}
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return s
	}
	tag := "data-" + hex.EncodeToString(b[:])
	return "<" + tag + ">\n" + s + "\n</" + tag + ">"
}
