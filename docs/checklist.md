# Чеклист готовности backend

Составлен по `api/openapi.yaml` (36 операций). Отмечено сделанное на 2026-10-26.

## Итог

| Метрика | Значение |
|---|---|
| Операций API реализовано | 7 из 36 (19%) |
| Готовность с учётом сложности | ≈ 13% |

Оценка по сложности: веса модулей — auth 4, users 2, forms 5, splits 9 (правила
состава, переходы статусов, SSE), invites 4, members 8, distribution 12
(алгоритм и property-based тесты), teams 3, health 0.5, тестовая обвязка 2.
Готово 6.5 из ≈ 49.5.

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

## Forms — 0/5

- [ ] Миграция `forms` (несколько анкет на пользователя)
- [ ] `GET /forms`
- [ ] `POST /forms`
- [ ] `GET /forms/{formId}` (чужая анкета — 404)
- [ ] `PATCH /forms/{formId}`
- [ ] `DELETE /forms/{formId}`

## Splits — 0/7

- [ ] Миграции `splits`, `split_roles`
- [ ] Валидация правил состава (`sum(perTeamMin) <= teamSize <= sum(perTeamMax)` и др.), 422 с `details`
- [ ] `GET /splits`
- [ ] `POST /splits`
- [ ] `GET /splits/{splitId}`
- [ ] `PATCH /splits/{splitId}`
- [ ] `DELETE /splits/{splitId}`
- [ ] `POST /splits/{splitId}/status` (Draft → Open → Closed → Finished)
- [ ] `GET /splits/{splitId}/stream` (SSE)

## Invites — 0/4

- [ ] Миграция `invites`
- [ ] `GET /splits/{splitId}/invites`
- [ ] `POST /splits/{splitId}/invites`
- [ ] `DELETE /splits/{splitId}/invites/{inviteId}`
- [ ] `GET /invites/{token}` (предпросмотр)

## Members — 0/7

- [ ] Миграция `members` (`UNIQUE(split_id, user_id)`, `team_id` и `assigned_role` nullable, `form_snapshot` jsonb)
- [ ] `POST /members/join` (заморозка анкеты в снимок)
- [ ] `GET /splits/{splitId}/members` (анкеты видны только владельцу)
- [ ] `GET /splits/{splitId}/members/me`
- [ ] `DELETE /splits/{splitId}/members/me`
- [ ] `PATCH /members/{memberId}`
- [ ] `DELETE /members/{memberId}`

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
- [ ] Остальные модули

## Известные долги

- Операции без реализации отвечают 500 (заглушка `notImplemented` в `cmd/api/server.go`), а не 501.
- Автоотзыв всех сессий пользователя и rate limiting сознательно не делаются до MVP.
