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

Готово: `auth` (регистрация, вход, обновление и отзыв сессии), `user`
(`GET /users/me`), `GET /health`. Остальные методы из спеки пока не
реализованы.

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

Как устроена авторизация:
- **Access-токен** (JWT, 30 минут) приходит в теле ответа `register`, `login` и
  `refresh`; его передают в заголовке `Authorization: Bearer <accessToken>`.
- **Refresh-токен** (30 дней) приходит только в httpOnly-cookie
  `refresh_token` (путь `/api/v1/auth`) и одноразовый: при каждом `refresh`
  выдаётся новый. Повторное использование старого токена отзывает всю
  цепочку, потребуется новый вход.
- Роли Owner/Member определяются внутри пространства, глобальной роли у
  пользователя нет.

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
