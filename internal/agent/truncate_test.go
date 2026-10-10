package agent

import "testing"

func longBody() string {
	s := "Начало работы. "
	for i := 0; i < 20; i++ {
		s += "это длинный ответ модели, который продолжается без остановки; "
	}
	return s
}

func TestLooksTruncated(t *testing.T) {
	if looksTruncated(longBody()+"поднимаю зап") != true {
		t.Fatal("оборванный на полуслове ответ должен считаться обрезанным")
	}
	if looksTruncated(longBody() + "задача выполнена.") {
		t.Fatal("ответ, завершённый точкой, не должен считаться обрезанным")
	}
	if looksTruncated(longBody() + "задача выполнена\n") {
		t.Fatal("ответ, завершённый переносом строки, не должен считаться обрезанным")
	}
	if looksTruncated("коротко") {
		t.Fatal("короткий текст не должен считаться обрезанным")
	}
}
