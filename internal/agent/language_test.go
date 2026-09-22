package agent

import "testing"

// Так выглядел реальный ответ: русский текст с китайскими вставками.
const mixedAnswer = "已完成，版本仍为 0.6.33。" +
	"已推送到 origin、kalinamall、github 三个仓库；标签 v0.6.33 已移动到同一提交。" +
	"已重新构建并重新上传到 GitHub 上的 v0.6.33 发布。"

func TestWrongLanguageMixedAnswer(t *testing.T) {
	if !wrongLanguage("коммит пуш и заливка релиза в ту же версию", mixedAnswer) {
		t.Fatal("mixed CJK answer must be detected")
	}
}

func TestWrongLanguagePureRussian(t *testing.T) {
	answer := "Готово. Релиз 0.6.33 перезалит, zip и подпись обновлены, рабочее дерево чистое."
	if wrongLanguage("сделай релиз", answer) {
		t.Fatal("russian answer must pass")
	}
}

// Пара иероглифов в имени файла — не повод переспрашивать.
func TestWrongLanguageFewCJKChars(t *testing.T) {
	answer := "Файл 文档.txt обновлён, в остальном всё по-русски и без иероглифов."
	if wrongLanguage("что там с файлом", answer) {
		t.Fatal("a few CJK chars must not trigger a retry")
	}
}

// Пользователь пишет по-китайски — значит и отвечать можно по-китайски.
func TestWrongLanguageChineseUser(t *testing.T) {
	if wrongLanguage("请帮我推送 релиз", mixedAnswer) {
		t.Fatal("chinese user request must not trigger a retry")
	}
}

// Английский ответ на русский запрос — не наша забота (проверяем только CJK).
func TestWrongLanguageEnglishAnswer(t *testing.T) {
	if wrongLanguage("сделай релиз", "Done: release 0.6.33 re-uploaded, assets updated.") {
		t.Fatal("english answer must not trigger a retry")
	}
}

func TestWrongLanguageEmptyAnswer(t *testing.T) {
	if wrongLanguage("сделай релиз", "") {
		t.Fatal("empty answer must not trigger a retry")
	}
	if wrongLanguage("", mixedAnswer) {
		t.Fatal("empty user text must not trigger a retry")
	}
}

func TestLetterCounts(t *testing.T) {
	letters, cyr, cjk := letterCounts("Готово 已完成")
	if letters != 9 || cyr != 6 || cjk != 3 {
		t.Fatalf("letters=%d cyr=%d cjk=%d", letters, cyr, cjk)
	}
}
