# TeamBuilder

Сервис формирования студенческих проектных команд. Преподаватель создаёт
пространство, задаёт правила состава (размер команды и роли с квотами) и
рассылает инвайт-ссылку. Студенты вступают с анкетой компетенций. По кнопке
или по таймеру запускается распределение по командам.

Монорепозиторий: бэкенд на Go (`backend/`), фронтенд-заготовка, общий
контракт OpenAPI (`api/openapi.yaml`). Правила разработки и границы модулей —
в [CLAUDE.md](CLAUDE.md).

## Стек

Go, echo, PostgreSQL 16, pgx + sqlc, goose, OpenAPI 3.0.3 + oapi-codegen,
JWT (access 30 минут + refresh в httpOnly-cookie), Argon2id, Docker.

## Статус

**Готовность backend: ≈ 30%** (по числу операций API — 14 из 36, 39%; с учётом
сложности модулей — ≈ 30%, самая тяжёлая часть, распределение, ещё не начата).
Подробный список задач — в [docs/checklist.md](docs/checklist.md).

| Сервис | Операции | Готовность | Комментарий |
|---|---|---|---|
| health | 1 из 1 | 100% | |
| auth | 4 из 4 | 100% | регистрация, вход, ротация refresh, выход |
| user | 2 из 2 | 100% | профиль, частичное обновление |
| split | 7 из 7 | 90% | события `member_joined` и `distribution_published` и реальные участники ждут модули `member` и `distribution` |
| forms | 0 из 5 | 0% | анкеты компетенций |
| invites | 0 из 4 | 0% | инвайт-ссылки |
| members | 0 из 6 | 0% | вступление, состав, выход |
| distribution | 0 из 3 | 0% | алгоритм, предпросмотр, публикация |
| teams | 0 из 3 | 0% | результат и выгрузка |
| тестовая обвязка | 0 из 1 | 0% | `POST /test/reset`, `make seed` |

Вес модуля в общей оценке пропорционален трудоёмкости: распределение — 12,
пространства — 9, участники — 8, анкеты — 5, auth и инвайты — по 4, команды — 3,
профиль и тестовая обвязка — по 2, health — 0,5.

## Quick start

Нужен только Docker.

```bash
make up        # БД, миграции и API на http://localhost:8080/api/v1
curl localhost:8080/api/v1/health
```

Регистрация и вход:

```bash
curl -c cookies.txt -H 'Content-Type: application/json' \
  -d '{"login":"ivan","password":"12345678","firstName":"Иван","lastName":"Иванов"}' \
  localhost:8080/api/v1/auth/register

# access-токен из ответа — в заголовок Authorization
curl -H "Authorization: Bearer <accessToken>" localhost:8080/api/v1/users/me

# обновить сессию по refresh-cookie
curl -b cookies.txt -c cookies.txt -X POST localhost:8080/api/v1/auth/refresh
```

Остановить: `make down`.

## Доступные API

База: `http://localhost:8080/api/v1`. Полный контракт — в
[api/openapi.yaml](api/openapi.yaml); реализованы только методы из таблицы,
остальные из спеки пока отвечают 500.

| Метод | Путь | Доступ | Назначение | Ответы |
|---|---|---|---|---|
| GET | `/health` | публичный | статус сервиса и БД | 200, 503 |
| POST | `/auth/register` | публичный | регистрация, сразу выдаёт сессию | 201, 400, 409 `login_taken`, 422 |
| POST | `/auth/login` | публичный | вход по логину и паролю | 200, 400, 401 `invalid_credentials` |
| POST | `/auth/refresh` | refresh-cookie | обновление сессии с ротацией refresh | 200, 401 |
| POST | `/auth/logout` | Bearer | выход, отзыв refresh и очистка cookie | 204, 401 |
| GET | `/users/me` | Bearer | профиль текущего пользователя | 200, 401 |
| PATCH | `/users/me` | Bearer | частичное обновление профиля (`firstName`, `lastName`, `middleName`, `link`) | 200, 400, 401, 422 |
| GET | `/splits` | Bearer | свои пространства и те, где вы участник; фильтры `role`, `status` | 200, 401 |
| POST | `/splits` | Bearer | создание (вы становитесь владельцем, статус `Draft`) | 201, 400, 401, 422 `invalid_team_rules` |
| GET | `/splits/{splitId}` | владелец, участник | пространство с ролями и счётчиками | 200, 401, 404 |
| PATCH | `/splits/{splitId}` | владелец | частичное обновление; `teamSize` и `roles` только в `Draft` | 200, 400, 401, 403, 404, 409, 422 |
| DELETE | `/splits/{splitId}` | владелец | удаление | 204, 401, 403, 404 |
| POST | `/splits/{splitId}/status` | владелец | `Draft→Open`, `Open→Closed`, `Closed→Open` | 200, 400, 401, 403, 404, 409 `invalid_status_transition` |
| GET | `/splits/{splitId}/stream` | владелец, участник | поток событий SSE | 200, 401, 404 |

Как устроена авторизация:
- **Access-токен** (JWT, 30 минут) приходит в теле ответа `register`, `login` и
  `refresh`; его передают в заголовке `Authorization: Bearer <accessToken>`.
- **Refresh-токен** (30 дней) приходит только в httpOnly-cookie
  `refresh_token` (путь `/api/v1/auth`) и одноразовый: при каждом `refresh`
  выдаётся новый. Повторное использование старого токена отзывает всю
  цепочку, потребуется новый вход.
- Роли Owner/Member определяются внутри пространства, глобальной роли у
  пользователя нет.

`PATCH /splits/{id}`: `null` в `description`, `maxMembers`, `autoFinishAt` очищает
значение, отсутствие поля его не меняет. В `Finished` пространство менять нельзя.
Чужое пространство отвечает 404, а не 403; 403 получает участник, пытающийся
сделать то, что доступно только владельцу.

`GET /splits/{id}/stream` отдаёт события `status_changed` (`{"status": "Open"}`)
и `ping` каждые 20 секунд. Авторизация идёт заголовком `Authorization`, поэтому
браузерный `EventSource` не подойдёт, нужен fetch-стрим:

```bash
curl -N -H "Authorization: Bearer <accessToken>" localhost:8080/api/v1/splits/1/stream
```

Пока нет модуля участников, `membersCount` всегда 0, а доступ к пространству
есть только у владельца (временная заглушка `memberStub` в `cmd/api`).

`PATCH /users/me` меняет только переданные поля. Для `middleName` и `link`
`null` или пустая строка очищает значение; `firstName` и `lastName` пустыми
быть не могут (422).

```bash
curl -X PATCH -H "Authorization: Bearer <accessToken>" -H 'Content-Type: application/json' \
  -d '{"firstName":"Пётр","link":null}' localhost:8080/api/v1/users/me
```

Ошибки приходят в едином формате `{code, message, details?, requestId}`.

## Логи

API пишет JSON-логи в stdout, на каждый запрос — запись уровня debug
(метод, путь, маршрут, статус, время, `request_id`). Заголовки, cookie и тела
запросов не логируются. Уровень задаёт `LOG_LEVEL` (`debug`, `info`, `warn`,
`error`); в docker-compose по умолчанию `debug`, в самом приложении — `info`.

```bash
docker compose logs -f api            # следить за логами API
docker compose logs --tail=100 api    # последние 100 строк
docker compose logs -f                # все сервисы сразу (api, db, migrate)
docker compose logs migrate           # результат миграций
```

Сменить уровень: `LOG_LEVEL=info make up`. Найти запрос по `request_id`
из ответа об ошибке: `docker compose logs api | grep <requestId>`.

## Разработка

Сгенерированный код (`backend/internal/api/gen`, `backend/internal/*/sqlc/db`)
в git не хранится. Docker-сборка генерирует его сама; для локальной работы:

```bash
make tools     # oapi-codegen и sqlc
make gen       # сгенерировать код из openapi.yaml и SQL-запросов
make test      # тесты (интеграционные поднимают Postgres через Docker)
make build
```

Конфиг — через env, пример в `.env.example` (`DATABASE_URL`, `JWT_SECRET` не
короче 32 байт, `PORT`, `APP_ENV`, `CORS_ORIGINS`, необязательные `LOG_LEVEL`, `ACCESS_TTL`
и `REFRESH_TTL`).

## Структура

```
api/openapi.yaml            контракт, источник истины
backend/cmd/api             точка входа, сборка модулей
backend/internal/<модуль>   auth, user, ... (domain, service, repository, handler, port, tests/)
backend/internal/platform   конфиг, обработка ошибок
backend/migrations          goose-миграции
```
