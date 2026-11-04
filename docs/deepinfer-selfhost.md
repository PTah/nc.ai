<!-- Created by nc.ai 1.1.3 on macbook 2026-11-04 14:28:08 -->
# DeepInfer — движок DeepSeek для своего железа (разбор)

Изучено по [github.com/deepseek-ai/DeepInfer](https://github.com/deepseek-ai/DeepInfer) (Apache-2.0,
октябрь 2026). Короткая версия: **это не сервер и не клиент, а offline-движок инференса для
DeepSeek-V4.1-Flash на кластере SM100 / Ascend 950.** Для NotCursor.ai в текущем виде бесполезен,
но три его возможности стоит забрать в продукт, когда они доедут до API — см. конец файла.

---

## Что это

Фреймворк, на котором DeepSeek крутит свои продакшн-сервисы; открытая часть — **offline inference**
(библиотека + `examples/offline_inference.py`). Построен в первую очередь на
[vLLM](https://github.com/vllm-project/vllm), с идеями из SGLang и TensorRT-LLM.

| Параметр | Значение |
|---|---|
| Модели | ровно одна: `DeepSeek-V4.1-Flash-1017` |
| Железо | NVIDIA **SM100** (CUDA 13) или Huawei **Ascend 950** (CANN 9.2) |
| ОС / тулчейн | Linux, GCC ≥ 13, Python 3.12 |
| Лицензия | Apache-2.0 |
| Бенчмарки в README | 4 GPU на prefill, 64 GPU на decode |

## Что умеет

| Возможность | Как включается / что даёт |
|---|---|
| Оптимизированные ядра | FlashMLA, DeepGEMM, DeepEP, TileKernels, DeepSelect, TileLang-ядра |
| Параллелизм | Attention DP / balanced CP, MoE EP + EPLB (эксперты не «перекашиваются») |
| DSpark + confidence-scheduled verification | `DEEPINFER_ENABLE_MTP_VARLEN=1`, длина драфта `DEEPINFER_USE_NEXTN_DECODE` (по умолчанию 5) |
| SWA bounded replay (prefill) | `DEEPINFER_YOCO_PREFILL_TRUNCATION_MODE=compact`, `DEEPINFER_YOCO_PREFILL_KEEP_TOKENS=140` |
| Асинхронный планировщик | включён по умолчанию: CPU-планирование перекрывается с работой устройства |
| Batch-invariant inference | `DEEPINFER_BATCH_INVARIANT=1` (только NVIDIA): результат не зависит от состава батча |
| Deterministic sampling | `DEEPINFER_BATCH_INVARIANT=1 DEEPINFER_ENABLE_MTP_VARLEN=0` + фиксированный `seq_seed` |
| Structured outputs | `so_args={"schema_type": …, "schema": …}` в `SamplingParams`: regex, JSON schema, EBNF |

## Чего в релизе нет (и почему это не сервер)

README прямо перечисляет, что нужно для полноценного inference-сервиса и **не входит**:

- система KV-кэша;
- VE / Prefill / Decode disaggregation (большая часть движковой части уже сделана);
- балансировка между data-parallel репликами для prefill и decode.

Без этого наружу нечего отдавать по HTTP — ни OpenAI-совместимого `/v1/chat/completions`, ни
`/v1/models`. Только Python API внутри процесса.

## Быстрый старт (как в README, для полноты)

```bash
python3.12 -m venv .venv && source .venv/bin/activate
export DEEPINFER_ACCELERATOR=nvidia     # или ascend
./build.sh
python examples/offline_inference.py    # чекпойнт скачается в кеш HF
```

Локальная копия весов вместо скачивания: `DEEPINFER_HF_MODEL_DIR=/path/to/DeepSeek-V4.1-Flash-1017`.

## Почему нам сейчас не подходит

1. **Нет HTTP-фронта.** Наш Local/Custom-провайдер (`internal/llm/providers/local`) работает только
   с OpenAI-совместимым API: `POST {base}/chat/completions`, `GET {base}/models`, опциональный
   Bearer — см. `docs/exchange-protocols/ollama.md` и `docs/local-models-ru.md`. Подключить
   DeepInfer в Settings → Local нечем: он не слушает порт.
2. **Нет Apple Silicon.** Только SM100 и Ascend 950; на маке (где собираются macOS-артефакты)
   он не запустится ни в каком режиме. Для локальных моделей остаются Ollama / LM Studio /
   llama.cpp.
3. **Порог входа — кластер.** Сборка из исходников под свой кернел-стек, десятки GPU на decode.
   Для сценария «модель крутится рядом, данные не уходят наружу» это на порядки больше нужного.

## Что забрать в продукт (идеи, не зависимости)

Эти три вещи решают наши реальные задачи и не требуют кластера — нужен только их аналог в API
провайдера (или наш собственный слой поверх ответа):

| Идея DeepInfer | Что это даёт нам | Где у нас болит сейчас |
|---|---|---|
| **Structured outputs** (regex / JSON schema / EBNF) | жёсткая форма ответа вместо «пожалуйста, верни JSON»: меньше повторов и парс-ошибок на 5–10 % шагов агента | `internal/agent/protocol.go` — разбор tool-call, ретраи на битом JSON, `escalate-invalid-tool` |
| **Deterministic sampling** (`seed`) | повторяемые прогоны: один и тот же ввод → один и тот же ответ, полезно для тестов агента и разбора инцидентов | тесты `internal/agent/*_test.go` фиксируют только промпт, ответ каждого прогона свой |
| **Batch-invariant** | воспроизводимость при разной загрузке: не «повезло с батчем» | сложно ловить регрессии по стоимости/качеству между прогонами |

Практический вывод: когда провайдер отдаст `response_format` / `seed` в публичном API (у DeepSeek,
Z.ai, OpenRouter это появляется неравномерно), добавляем их в `internal/llm` как опциональные поля
запроса, а в агенте — как «строгий режим» для tool-call и для `review`-субагентов. Отдельного слоя
поверх DeepInfer для этого не нужно: он всё равно не сервер.

## Если всё же понадобится свой DeepSeek-V4.1-Flash

Сценарий «мы сами держим модель и раздаём её всей команде» решается так:

1. **Сейчас** — vLLM (или SGLang) с OpenAI-совместимым сервером: наш Local-провайдер уже работает
   с ним без изменений кода, достаточно `base_url` в Settings → Local.
2. **Когда у DeepInfer появится HTTP-фронт** (KV-кэш, disaggregation, балансировка из списка
   «не входит») — он станет ещё одним `base_url`. Изменений в приложении не потребуется, кроме
   подсказок про окно контекста, если у сервера свои лимиты (как сейчас для Ollama/LM Studio в
   `app.go:4436`).
3. **Когда появится SM100/Ascend 950** — тогда имеет смысл сравнивать DeepInfer с vLLM по
   пропускной способности на своей нагрузке, а не по картинкам из README.

Триггеры пересмотра этого файла: (а) OpenAI-совместимый сервер в DeepInfer; (б) своё железо
SM100/Ascend 950; (в) `seed` и `response_format` в публичном API DeepSeek.
