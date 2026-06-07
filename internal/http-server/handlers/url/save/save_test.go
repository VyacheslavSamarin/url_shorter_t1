package save_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"url-shortener/internal/http-server/handlers/url/save"
	mocks "url-shortener/internal/http-server/handlers/url/save/mocks"
	"url-shortener/internal/lib/logger/handlers/slogdiscard"
	"url-shortener/internal/storage"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestSaveHandler(t *testing.T) {
	cases := []struct {
		name      string
		alias     string
		url       string
		respError string
		mockError error
		status    int
	}{
		{
			name:   "Success",
			alias:  "test_alias",
			url:    "https://google.com",
			status: http.StatusOK,
		},
		{
			name:   "Empty alias - random alias generated",
			alias:  "",
			url:    "https://google.com",
			status: http.StatusOK,
		},
		{
			name:      "Empty URL",
			url:       "",
			alias:     "some_alias",
			respError: "field Url is a required field",
			status:    http.StatusBadRequest,
		},
		{
			name:      "Invalid URL",
			url:       "some invalid URL",
			alias:     "some_alias",
			respError: "field Url is not a valid URL",
			status:    http.StatusBadRequest,
		},
		{
			name:      "SaveURL Error",
			alias:     "test_alias",
			url:       "https://google.com",
			respError: "failed to add url",
			mockError: errors.New("unexpected error"),
			status:    http.StatusInternalServerError,
		},
		{
			name:      "Alias already exists",
			alias:     "existing_alias",
			url:       "https://google.com",
			respError: "alias already exists",
			mockError: storage.ErrURLExists,
			status:    http.StatusConflict,
		},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockSaver := mocks.NewMockUrlSaver(ctrl)

			if tc.status == http.StatusOK {
				mockSaver.EXPECT().
					SaveUrl(tc.url, gomock.Any()).
					Return(tc.mockError).
					Times(1)
			} else if tc.mockError != nil {
				mockSaver.EXPECT().
					SaveUrl(tc.url, tc.alias).
					Return(tc.mockError).
					Times(1)
			}

			handler := save.New(slogdiscard.NewDiscardLogger(), mockSaver)

			input := fmt.Sprintf(`{"url": "%s", "alias": "%s"}`, tc.url, tc.alias)

			req, err := http.NewRequest(http.MethodPost, "/save", strings.NewReader(input))
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			require.Equal(t, tc.status, rr.Code)

			var resp save.Response
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))

			require.Equal(t, tc.respError, resp.Error)

			if tc.status == http.StatusOK {
				require.NotEmpty(t, resp.Alias)
			}

		})
	}
}
