# Чеклист готовности backend

Составлен по `api/openapi.yaml` (36 операций). Отмечено сделанное на 2026-10-26, после `invite` и `member` обновлено.

## Итог

| Метрика | Значение |
|---|---|
| Операций API реализовано | 28 из 36 (78%) |
| Готовность с учётом сложности | ≈ 62% |

Оценка по сложности: веса модулей — auth 4, users 2, forms 5, splits 9 (правила
состава, переходы статусов, SSE), invites 4, members 8, distribution 12
(алгоритм и property-based тесты), teams 3, health 0.5, тестовая обвязка 2.
Готово ≈ 30.9 из ≈ 49.5: `split` засчитан на 95% (нет только события
`distribution_published`), `member` — на 85% (8 × 0.85: `PATCH /members/{memberId}`
сознательно отложен).

## Инфраструктура

- [x] Docker Compose (БД, миграции, API), Dockerfile с кодогенерацией
- [x] Конфиг из env, JSON-логи, debug-лог каждого запроса
- [x] Единый формат ошибок (`Error`), централизованный маппинг в `platform`
- [x] Кодогенерация: oapi-codegen (strict server, echo v5) и sqlc
- [x] Тестовый хелпер Postgres (`platform/pgtest`)
- [ ] `make seed`: 1 преподаватель, 8 студентов, 1 пространство
- [ ] `POST /test/reset` (только при `APP_ENV=test`)
- [ ] CORS и cookie проверены с настоящим фронтендом
- [ ] CI (тесты, сборка)

## Health — 1/1

- [x] `GET /health`

## Auth — 4/4

- [x] `POST /auth/register`
- [x] `POST /auth/login`
- [x] `POST /auth/refresh` (ротация, обнаружение повторного использования)
- [x] `POST /auth/logout`

## Users — 2/2

- [x] `GET /users/me`
- [x] `PATCH /users/me`
- [x] `ListBriefs` для других модулей (нужен для `UserBrief`)

## Forms — 5/5

- [x] Миграция `forms` (несколько анкет на пользователя)
- [x] `GET /forms`
- [x] `POST /forms`
- [x] `GET /forms/{formId}` (чужая анкета — 404)
- [x] `PATCH /forms/{formId}`
- [x] `DELETE /forms/{formId}`
- [x] `Service.Snapshot` для заморозки анкеты при вступлении (подключит `member` через свой порт)

## Splits — 7/7

- [x] Миграции `splits`, `split_roles`
- [x] Валидация правил состава (`sum(perTeamMin) <= teamSize <= sum(perTeamMax)` и др.), 422 `invalid_team_rules` с `details`
- [x] `GET /splits`
- [x] `POST /splits`
- [x] `GET /splits/{splitId}`
- [x] `PATCH /splits/{splitId}`
- [x] `DELETE /splits/{splitId}`
- [x] `POST /splits/{splitId}/status` (Draft → Open → Closed → Open)
- [x] `GET /splits/{splitId}/stream` (SSE: `status_changed`, `ping`)
- [x] Событие `member_joined` (публикует `member` через `Hub`)
- [ ] Событие `distribution_published` (опубликует `distribution` через `Hub`)
- [x] Реальные участники вместо заглушки `memberStub` (`membersCount`, `myRole=Member`, доступ участника)

## Invites — 4/4

- [x] Миграция `invites` (`ON DELETE CASCADE` от `splits`)
- [x] `GET /splits/{splitId}/invites`
- [x] `POST /splits/{splitId}/invites`
- [x] `DELETE /splits/{splitId}/invites/{inviteId}` (идемпотентный)
- [x] `GET /invites/{token}` (предпросмотр)
- [x] `Service.Redeem` / `Release` (атомарный счётчик) для `member`, подключит он через свой порт

## Members — 5/6

- [x] Миграция `members` (`UNIQUE(split_id, user_id)`, `team_id` и `assigned_role` nullable, `form_snapshot` jsonb)
- [x] `POST /members/join` (заморозка анкеты в снимок; возврат использования инвайта при отказе)
- [x] `GET /splits/{splitId}/members` (анкеты видны только владельцу)
- [x] `GET /splits/{splitId}/members/me`
- [x] `DELETE /splits/{splitId}/members/me`
- [ ] `PATCH /members/{memberId}` — отложено: ручной перенос между командами сознательно не делается до MVP (CLAUDE.md)
- [x] `DELETE /members/{memberId}`
- [x] Сериализация вступлений: `pg_advisory_xact_lock(split_id)` + мьютекс в памяти; `distribution.publish` должен брать тот же lock (`SELECT pg_advisory_xact_lock(split_id)` до проверки статуса)
- [ ] Для `distribution`: метод `ListCandidates` (снимки анкет) и FK `members.team_id → teams.id` (в миграции модуля `team`)

## Distribution — 0/3

- [ ] Миграция `distribution_runs` (`seed`)
- [ ] Алгоритм распределения (квоты ролей, `remainderPolicy`, лист ожидания)
- [ ] `POST /splits/{splitId}/distribution/preview`
- [ ] `POST /splits/{splitId}/distribution/publish` (идемпотентность: `FOR UPDATE` и проверка статуса в транзакции)
- [ ] `GET /splits/{splitId}/distribution/runs`
- [ ] Property-based тесты инвариантов (одна команда на участника, квоты, размеры, детерминизм по `seed`)

## Teams — 0/3

- [ ] Миграция `teams`
- [ ] `GET /splits/{splitId}/teams`
- [ ] `GET /splits/{splitId}/teams/me`
- [ ] `GET /splits/{splitId}/teams/export`

## Тесты

- [x] auth: юнит и интеграционные (12 функций)
- [x] user: юнит, HTTP-разбор тела, интеграционные
- [ ] HTTP-слой auth (cookie, middleware) автотестами
- [x] split: правила состава (в том числе сверка с перебором), статусы, доступ, HTTP, SSE, Postgres, гонка смены статуса
- [x] member: домен, сервис (в том числе гонка вступлений и отмена ожидания), HTTP и приватность анкет, Postgres (каскады, снимок, гонка при пуле в 4 соединения)
- [ ] Остальные модули

## Известные долги

- Операции без реализации отвечают 500 (заглушка `notImplemented` в `cmd/api/server.go`), а не 501.
- Автоотзыв всех сессий пользователя и rate limiting сознательно не делаются до MVP.
