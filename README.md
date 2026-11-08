# drill

Тренажёр без ИИ в духе BBS: подключаешься по SSH, логин — твой публичный ключ,
внутри план на полгода, задачи в стиле LeetCode с проверкой и мини-vim.

```
ssh -p 2222 drill.example.dev
```

## Что внутри

- **Вход по ключу.** Новый ключ → экран регистрации ника. Второй ключ привязывается
  одноразовым кодом из раздела «Ключи» (`link КОД` вместо ника).
- **Задачи.** 10 задач на ноябрь (Arrays & Hashing, Two Pointers, Sliding Window),
  решаются на Go. `:run` гоняет открытые тесты, `:submit` все, включая большие
  скрытые: решение за O(n²) там упирается в лимит 1 с.
- **Редактор.** Normal / Insert / Command, `hjkl w b e 0 ^ $ gg G`, `x D J dd yy p P`,
  `dw cw cc d$`, счётчики (`3dd`), `u` / `ctrl+r`, автоотступ (табы для Go,
  4 пробела для Python), подсветка chroma, bracketed paste. Справка — `?`.
- **План.** Шесть месяцев из «План на полгода: Python, Go, Rust без костылей»,
  пункты отмечаются пробелом.
- **Прогресс.** Стрик по дням с принятой задачей или отмеченным пунктом плана,
  статистика по сложности и языкам, последние посылки. Черновики сохраняются
  на `:w`, `:run`, `:submit`, `:q`.

## Как устроено

```
ssh ──▶ wish (SSH, ключи) ──▶ bubbletea TUI ──▶ Postgres (пользователи, черновики, посылки, план)
                                   │
                                   └──▶ очередь судьи (2 воркера, 1 посылка на человека)
                                             │
                                             └──▶ docker run: без сети, 256 МБ, 1 CPU,
                                                  128 pids, cap-drop ALL, uid 65534, read-only /src
```

| Каталог | Что там |
| --- | --- |
| `cmd/drill` | сервер |
| `internal/ui` | экраны TUI |
| `internal/editor` | мини-vim (с тестами) |
| `internal/judge` | сборка и запуск решений, сравнение ответов, очередь |
| `internal/store` | Postgres через pgx, миграции в `migrations/` применяются при старте |
| `internal/catalog` | загрузка `tasks/` и `plan.yaml` |
| `tasks/<slug>/` | `task.yaml`, `statement.md`, `tests.json`, `go/stub.go`, `go/harness.go` |
| `sandbox/` | образ песочницы `drill-go:1` с прогретым кэшем stdlib |
| `tools/gentests` | генератор тестов и эталонные решения (**спойлеры**) |

## Запуск

Нужны Docker и Postgres 16.

```bash
make sandbox-image                 # образ песочницы, один раз
docker compose up -d db            # Postgres
make run-local                     # сервер на :2222
ssh -p 2222 localhost
```

Или всё в контейнерах: `docker compose up -d`. Сервер тогда запускает песочницы
через сокет хоста, поэтому каталог `/var/lib/drill/jobs` монтируется по тому же
пути. Доступ к docker.sock — это root на хосте, так что для чего-то публичного
лучше запускать бинарник под systemd на хосте или вынести судью на отдельную машину.

Переменные: `DRILL_ADDR` (`:2222`), `DRILL_DB`, `DRILL_CONTENT` (каталог с `tasks/`
и `plan.yaml`), `DRILL_HOSTKEY`, `DRILL_SANDBOX` (`docker` | `podman` | `local`),
`DRILL_RUNTIME` (например `runsc` для gVisor), `DRILL_JOBS_DIR`.

`DRILL_SANDBOX=local` запускает решения прямо на хосте без изоляции. Только для разработки.

## Тесты

```bash
go test ./...                                   # редактор, сравнение ответов, локальный судья
DRILL_TEST_DOCKER=1 DRILL_TEST_DB=postgres://... go test -count=1 ./...
```

С Docker проверяется: таймаут (O(n²) на two-sum), OOM, отсутствие сети, read-only
исходники, и что эталонное решение каждой задачи проходит все тесты, а заготовка нет.

## Новая задача

1. `tasks/<slug>/task.yaml`, `statement.md` (своими словами, ссылка на оригинал).
2. `go/stub.go` — сигнатура функции; `go/harness.go` — `func __call(a []json.RawMessage) any`,
   который разбирает аргументы через `__arg` и вызывает функцию.
3. Генератор в `tools/gentests/main.go` + эталон в `tools/gentests/solutions/<slug>.go.txt`.
4. `make tests && make test-all`.

`compare: unordered` сравнивает ответ-массив как мультимножество (two-sum, top-k).

## Что дальше

- Раннеры для Python (декабрь) и Rust (февраль): образ, скрипт в `judge/sandbox.go`, harness.
- Задачи на чтение кода: фрагмент и вопрос «что выведет».
- Режим собеседования: таймер 45 минут, без `:run` до первого `:submit`.
- Редактор v1: Visual-режим, поиск `/`, `:fmt`.
