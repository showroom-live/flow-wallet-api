package tests

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/accounts"
	"github.com/flow-hydraulics/flow-wallet-api/flow_helpers"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/tests/internal/test"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/gorilla/mux"
	"github.com/onflow/flow-go-sdk"
)

func TestEmulatorAcceptsSignedTransaction(t *testing.T) {
	cfg := test.LoadConfig(t, testConfigPath)
	svcs := test.GetServices(t, cfg)

	accHandler := handlers.NewAccounts(test.Logger(), svcs.GetAccounts())
	txHandler := handlers.NewTransactions(test.Logger(), svcs.GetTransactions())

	router := mux.NewRouter()
	router.Handle("/", accHandler.Create()).Methods(http.MethodPost)
	router.Handle("/{address}/sign", txHandler.Sign()).Methods(http.MethodPost)

	// Create signing account.
	var account accounts.Account
	res := send(router, http.MethodPost, "/?sync=true", nil)
	assertStatusCode(t, res, http.StatusCreated)
	fromJsonBody(res, &account)

	// Transaction:
	code := "transaction(greeting: String) { prepare(signer: AuthAccount){} execute { log(greeting.concat(\", World!\")) }}"
	args := "[{\"type\":\"String\",\"value\":\"Hello\"}]"

	// Sign it.
	body := bytes.NewBufferString(fmt.Sprintf("{\"code\":%q,\"arguments\":%s}", code, args))
	res = send(router, http.MethodPost, fmt.Sprintf("/%s/sign", account.Address), body)
	assertStatusCode(t, res, http.StatusCreated)

	var txResp transactions.SignedTransactionJSONResponse
	err := json.NewDecoder(res.Body).Decode(&txResp)
	if err != nil {
		t.Fatal("failed to decode transaction signing response")
	}

	tx := flow.NewTransaction().
		SetScript([]byte(txResp.Code)).
		SetReferenceBlockID(flow.HexToID(txResp.ReferenceBlockID)).
		SetGasLimit(txResp.GasLimit).
		SetProposalKey(flow.HexToAddress(txResp.ProposalKey.Address), txResp.ProposalKey.KeyIndex, txResp.ProposalKey.SequenceNumber).
		SetPayer(flow.HexToAddress(txResp.Payer))

	for _, arg := range txResp.Arguments {
		tx.AddRawArgument(arg)
	}

	for _, a := range txResp.Authorizers {
		tx.AddAuthorizer(flow.HexToAddress(a))
	}

	for _, s := range txResp.PayloadSignatures {
		bs, err := hex.DecodeString(s.Signature)
		if err != nil {
			t.Fatal(err)
		}
		tx.AddPayloadSignature(flow.HexToAddress(s.Address), s.KeyIndex, bs)
	}

	for _, s := range txResp.EnvelopeSignatures {
		bs, err := hex.DecodeString(s.Signature)
		if err != nil {
			t.Fatal(err)
		}
		tx.AddEnvelopeSignature(flow.HexToAddress(s.Address), s.KeyIndex, bs)
	}

	ctx := context.Background()
	client := test.NewFlowClient(t, cfg)
	_, err = flow_helpers.SendAndWait(ctx, client, *tx, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
}

func assertStatusCode(t *testing.T, res *http.Response, expected int) {
	t.Helper()
	if res.StatusCode != expected {
		bs, err := ioutil.ReadAll(res.Body)
		if err != nil {
			panic(err)
		}
		t.Fatalf("expected HTTP response status code %d, got %d: %s", expected, res.StatusCode, string(bs))
	}
}

func asJson(v interface{}) []byte {
	if v == nil {
		return nil
	}
	bs, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return bs
}

func fromJsonBody(res *http.Response, v interface{}) {
	bs, err := ioutil.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}

	err = json.Unmarshal(bs, v)
	if err != nil {
		panic(err)
	}
}

func send(router *mux.Router, method, path string, body io.Reader) *http.Response {
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr.Result()
}
