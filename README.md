# TeamBuilder

Сервис формирования студенческих проектных команд. Преподаватель создаёт
пространство, задаёт правила состава (размер команды и роли с квотами) и
рассылает инвайт-ссылку. Студенты вступают с анкетой компетенций. По кнопке
запускается распределение по командам (автозапуск по таймеру после MVP).

Монорепозиторий: бэкенд на Go (`backend/`), фронтенд-заготовка, общий
контракт OpenAPI (`api/openapi.yaml`). Правила разработки и границы модулей —
в [CLAUDE.md](CLAUDE.md).

## Стек

Go, echo, PostgreSQL 16, pgx + sqlc, goose, OpenAPI 3.0.3 + oapi-codegen,
JWT (access 30 минут + refresh в httpOnly-cookie), Argon2id, Docker.

## Статус

**Готовность backend: ≈ 94%** (по числу операций API — 34 из 36, 94%; с учётом
сложности модулей — ≈ 94%). Не сделаны `PATCH /members/{memberId}` (ручной
перенос участников между командами отложен до MVP), `POST /test/reset`,
`make seed` и CI-сборка стенда. Подробный список задач — в
[docs/checklist.md](docs/checklist.md).

| Сервис | Операции | Готовность | Комментарий |
|---|---|---|---|
| health | 1 из 1 | 100% | |
| auth | 4 из 4 | 100% | регистрация, вход, ротация refresh, выход |
| user | 2 из 2 | 100% | профиль, частичное обновление |
| split | 7 из 7 | 100% | пространства, правила состава, статусы, SSE (`status_changed`, `member_joined`, `distribution_published`) |
| forms | 5 из 5 | 100% | анкеты компетенций |
| invites | 4 из 4 | 100% | выпуск, список, отзыв, публичный предпросмотр |
| members | 5 из 6 | 85% | вступление по инвайту, состав, выход, исключение; `PATCH` отложен |
| distribution | 3 из 3 | 100% | алгоритм `greedy_v1`, предпросмотр, публикация, история прогонов |
| teams | 3 из 3 | 100% | команды, моя команда, выгрузка CSV |
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
[api/openapi.yaml](api/openapi.yaml); реализованы все методы из таблицы.
Не реализованы `PATCH /members/{memberId}` и `POST /test/reset`: они пока
отвечают 500.

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
| GET | `/forms` | Bearer | свои анкеты | 200, 401 |
| POST | `/forms` | Bearer | создание анкеты | 201, 400, 401, 422 |
| GET | `/forms/{formId}` | владелец анкеты | получение; чужая анкета — 404 | 200, 401, 404 |
| PATCH | `/forms/{formId}` | владелец анкеты | частичное обновление; `skills` и `rolePreferences` заменяются целиком | 200, 400, 401, 404, 422 |
| DELETE | `/forms/{formId}` | владелец анкеты | удаление | 204, 401, 404 |
| GET | `/splits/{splitId}/invites` | владелец | список инвайтов, включая отозванные | 200, 401, 403, 404 |
| POST | `/splits/{splitId}/invites` | владелец | выпуск инвайт-ссылки; тело необязательно | 201, 400, 401, 403, 404, 422 |
| DELETE | `/splits/{splitId}/invites/{inviteId}` | владелец | отзыв; повторный отзыв тоже 204 | 204, 401, 403, 404 |
| GET | `/invites/{token}` | публичный | предпросмотр пространства без входа | 200, 404 `invite_not_found` |
| POST | `/members/join` | Bearer | вступление по инвайту, анкета замораживается в снимок | 201, 400, 401, 404 `invite_not_found`/`form_not_found`, 409 `already_member`/`split_full`/`split_not_open` |
| GET | `/splits/{splitId}/members` | владелец | участники со снимками анкет | 200, 401, 403, 404 |
| GET | `/splits/{splitId}/members/me` | участник | своё участие, команда и роль после публикации | 200, 401, 404 |
| DELETE | `/splits/{splitId}/members/me` | участник | выход, пока распределение не опубликовано | 204, 401, 404, 409 `split_finished` |
| DELETE | `/members/{memberId}` | владелец | исключение участника | 204, 401, 403, 404, 409 `split_finished` |
| POST | `/splits/{splitId}/distribution/preview` | владелец | пробный прогон без публикации; тело необязательно (`seed`) | 200, 401, 403, 404, 409 `not_enough_members`/`distribution_infeasible`/`split_not_open`/`already_finished` |
| POST | `/splits/{splitId}/distribution/publish` | владелец | публикация: команды, роли, статус `Finished`; идемпотентна | 200, 401, 403, 404, 409 `roster_changed`/`not_enough_members`/`distribution_infeasible`/`split_not_open` |
| GET | `/splits/{splitId}/distribution/runs` | владелец | история прогонов | 200, 401, 403, 404 |
| GET | `/splits/{splitId}/teams` | владелец | все команды с составами, после публикации | 200, 401, 403, 404, 409 `split_not_finished` |
| GET | `/splits/{splitId}/teams/me` | участник | моя команда с контактами сокомандников | 200, 401, 404 `not_assigned`, 409 `split_not_finished` |
| GET | `/splits/{splitId}/teams/export` | владелец | CSV: команда, фамилия, имя, контакт, роль | 200, 401, 403, 409 `split_not_finished` |

Как устроена авторизация:
- **Access-токен** (JWT, 30 минут) приходит в теле ответа `register`, `login` и
  `refresh`; его передают в заголовке `Authorization: Bearer <accessToken>`.
- **Refresh-токен** (30 дней) приходит только в httpOnly-cookie
  `refresh_token` (путь `/api/v1/auth`) и одноразовый: при каждом `refresh`
  выдаётся новый. Повторное использование старого токена отзывает всю
  цепочку, потребуется новый вход.
- Роли Owner/Member определяются внутри пространства, глобальной роли у
  пользователя нет.

Инвайты: `url` собирается как `INVITE_BASE_URL/join/<токен>`. Без тела
`POST` выпускает бессрочный инвайт без лимита; `expiresAt` должен быть в будущем,
`maxUses` не меньше 1. Предпросмотр отвечает одинаковым 404 `invite_not_found`,
если инвайт неизвестен, отозван, истёк или исчерпан. Посторонний получает 404,
участник пространства — 403.

Анкеты: у пользователя их может быть несколько. `PATCH /forms/{id}`: `null` в
`comment` очищает его, отсутствие поля не меняет; `skills` и `rolePreferences`
заменяются целиком. Навыки без учёта регистра дедуплицируются, предпочтения
сортируются по `rank`. Анкету можно менять и удалять в любой момент: при
вступлении в пространство её снимок замораживается у участника.

`PATCH /splits/{id}`: `null` в `description`, `maxMembers`, `autoFinishAt` очищает
значение, отсутствие поля его не меняет. В `Finished` пространство менять нельзя.
Чужое пространство отвечает 404, а не 403; 403 получает участник, пытающийся
сделать то, что доступно только владельцу.

`GET /splits/{id}/stream` отдаёт события `status_changed` (`{"status": "Open"}`),
`member_joined` (`{"membersCount": 5}`, при вступлении, выходе и исключении),
`distribution_published` (`{"runId": 12}`) и `ping` каждые 20 секунд. Авторизация идёт заголовком `Authorization`, поэтому
браузерный `EventSource` не подойдёт, нужен fetch-стрим:

```bash
curl -N -H "Authorization: Bearer <accessToken>" localhost:8080/api/v1/splits/1/stream
```

Участник видит пространство и поток событий, `myRole` для него `Member`;
`membersCount` и `teamsCount` (после публикации) заполняются из модулей
`member` и `team`.

**Участники.** Вступление (`POST /members/join`) проверяет инвайт, статус `Open`,
свободные места и повтор. Владелец своё пространство не вступает (409
`already_member`). Использование инвайта занимается до проверок и
возвращается при любом отказе, поэтому лимит инвайта расходуется только на
принятых. Анкета замораживается в `form_snapshot`; её правка или удаление
потом не влияют на распределение. Вступления и выходы сериализуются
advisory-lock пространства, так что `maxMembers` не превышается при
одновременных запросах. Анкеты других участников видит только владелец
(`GET /splits/{id}/members`); участник получает 403, посторонний — 404.
Выйти или быть исключённым после публикации нельзя (409 `split_finished`).

**Распределение.** Работает в статусах `Open` и `Closed`; автозапуск по таймеру
не реализован.
- `preview` сохраняет прогон, но ничего не публикует: ни команд, ни смены
  статуса. Прогон детерминирован: одинаковый `seed` на одном составе даёт
  одинаковый результат. Без `seed` сервер генерирует его (в диапазоне
  `[0, 2^53)`) и возвращает в ответе.
- `publish` одной транзакцией создаёт команды, проставляет участникам команду
  и роль и переводит пространство в `Finished`. Повторный вызов и
  одновременные вызовы не создают второй набор команд: возвращается уже
  опубликованный прогон. Если передан `runId`, публикуется именно этот прогон;
  если состав или правила поменялись после `preview`, отвечает 409
  `roster_changed`, нужен новый `preview`.
- `remainderPolicy` решает судьбу остатка: `waitlist` — лишние (вступившие
  последними) идут в лист ожидания, `spread` — равномерно по командам сверх
  размера, `smaller_last_team` — последняя команда меньше (при нехватке
  добирается из полных). Если состав нельзя разбить с соблюдением квот ролей,
  отвечает 409 `distribution_infeasible` с причиной в `details`.
- `metrics` в ответе: число команд, распределённых и в листе ожидания, соблюдены
  ли квоты, доля участников с ролью первого выбора, разброс среднего опыта.

**Команды.** Доступны только после публикации (до неё 409 `split_not_finished`).
`teams/me` отдаёт имена, контакты и роли сокомандников, анкет там нет.
Участник из листа ожидания получает 404 `not_assigned`. Выгрузка — CSV в
UTF-8 с BOM (кириллица открывается в Excel); значения, начинающиеся с `=`, `+`,
`-`, `@`, получают ведущий апостроф как защита от формул.

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

## CI

На каждый push и pull request GitHub Actions
([.github/workflows/test.yml](.github/workflows/test.yml)) устанавливает
генераторы, генерирует код, проверяет `gofmt` и `go vet`, собирает проект и
запускает тесты (включая интеграционные с Postgres в Docker). Результат виден
на вкладке **Actions** репозитория и рядом с коммитом или pull request.

## Разработка

Сгенерированный код (`backend/internal/api/gen`, `backend/internal/*/sqlc/db`)
в git не хранится. Docker-сборка генерирует его сама; для локальной работы:

```bash
make tools     # oapi-codegen и sqlc
make gen       # сгенерировать код из openapi.yaml и SQL-запросов
make test      # тесты (интеграционные и сквозные поднимают Postgres через Docker)
make build
```

Тесты лежат рядом с модулями (`backend/internal/<модуль>/tests/`); сквозные
сценарии на настоящем Postgres — в `backend/cmd/api/e2e_test.go`, инварианты
алгоритма распределения проверяются property-тестом на случайных входах.

Конфиг — через env, пример в `.env.example` (`DATABASE_URL`, `JWT_SECRET` не
короче 32 байт, `PORT`, `APP_ENV`, `CORS_ORIGINS`, необязательные `LOG_LEVEL`, `ACCESS_TTL`,
`REFRESH_TTL` и `INVITE_BASE_URL` — адрес фронтенда для инвайт-ссылок).

## Структура

```
api/openapi.yaml            контракт, источник истины
backend/cmd/api             точка входа, сборка модулей
backend/internal/<модуль>   auth, user, split, form, invite, member, distribution, team
                            (domain, service, repository, handler, port, tests/)
backend/internal/platform   конфиг, обработка ошибок, общая транзакция (TxManager)
backend/migrations          goose-миграции
```
