package tests

import (
	"context"
	"encoding/csv"
	"errors"
	"strings"
	"testing"

	"builder/backend/internal/team"
)

func TestMy(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		user    int64
		status  string
		want    error
		wantIDs []int64 // участники команды
	}{
		{"участник видит свою команду", alice, "Finished", nil, []int64{alice, bob}},
		{"сокомандник видит ту же команду", bob, "Finished", nil, []int64{alice, bob}},
		{"другая команда", carol, "Finished", nil, []int64{carol}},
		{"до публикации", alice, "Open", team.ErrNotFinished, nil},
		{"лист ожидания", dave, "Finished", team.ErrNotAssigned, nil},
		{"владелец не в команде", owner, "Finished", team.ErrNotAssigned, nil},
		{"посторонний получает 404", outside, "Finished", team.ErrSplitNotFound, nil},
		{"посторонний не узнаёт о статусе", outside, "Open", team.ErrSplitNotFound, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := newService(tc.status).My(ctx, tc.user, splitID)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				return
			}
			var ids []int64
			for _, m := range got.Members {
				ids = append(ids, m.User.ID)
			}
			if len(ids) != len(tc.wantIDs) {
				t.Fatalf("состав = %v, want %v", ids, tc.wantIDs)
			}
			for i := range ids {
				if ids[i] != tc.wantIDs[i] {
					t.Fatalf("состав = %v, want %v", ids, tc.wantIDs)
				}
			}
		})
	}
}

func parseCSV(t *testing.T, data []byte) [][]string {
	t.Helper()
	if len(data) < 3 || data[0] != 0xEF || data[1] != 0xBB || data[2] != 0xBF {
		t.Fatal("нет UTF-8 BOM: Excel покажет кириллицу кракозябрами")
	}
	rows, err := csv.NewReader(strings.NewReader(string(data[3:]))).ReadAll()
	if err != nil {
		t.Fatalf("невалидный CSV: %v\n%s", err, data)
	}
	return rows
}

func TestExport(t *testing.T) {
	ctx := context.Background()

	t.Run("состав, лист ожидания и заголовок", func(t *testing.T) {
		data, err := newService("Finished").Export(ctx, owner, splitID)
		if err != nil {
			t.Fatal(err)
		}
		rows := parseCSV(t, data)
		if len(rows) != 5 { // заголовок + 3 в командах + 1 в листе ожидания
			t.Fatalf("строк %d:\n%s", len(rows), data)
		}
		if strings.Join(rows[0], "|") != "Команда|Фамилия|Имя|Контакт|Роль" {
			t.Errorf("заголовок = %v", rows[0])
		}
		if rows[1][0] != "Команда 1" || rows[1][1] != "Иванова" || rows[1][4] != "Backend" {
			t.Errorf("первая строка = %v", rows[1])
		}
		last := rows[4]
		if last[0] != "" || last[1] != "Кузнецов" || last[4] != "" {
			t.Errorf("лист ожидания = %v", last)
		}
	})

	for _, tc := range []struct {
		name   string
		user   int64
		status string
		want   error
	}{
		{"до публикации", owner, "Closed", team.ErrNotFinished},
		{"участник", alice, "Finished", team.ErrForbidden},
		{"посторонний", outside, "Finished", team.ErrSplitNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := newService(tc.status).Export(ctx, tc.user, splitID)
			if !errors.Is(err, tc.want) || data != nil {
				t.Fatalf("err = %v, данных %d байт", err, len(data))
			}
		})
	}
}
