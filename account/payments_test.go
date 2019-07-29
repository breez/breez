package account

import (
	"fmt"
	"testing"

	"github.com/breez/breez/data"
)

func TestOnlyDescription(t *testing.T) {
	invoiceMemo := &data.InvoiceMemo{
		Description: "test",
	}
	description := formatTextMemo(invoiceMemo)
	if description != "test" {
		t.Fatalf("description should be test, instead got %v", description)
	}

	invoiceMemo = &data.InvoiceMemo{}
	parseTextMemo(description, invoiceMemo)
	if invoiceMemo.Description != "test" {
		t.Fatalf("invoiceMemo.Description should be test, instead got %v", invoiceMemo.Description)
	}
}

func TestWithPayeeData(t *testing.T) {
	invoiceMemo := &data.InvoiceMemo{
		Description:   "test",
		PayeeName:     "payee",
		PayeeImageURL: "payee_image",
	}
	description := formatTextMemo(invoiceMemo)
	invoiceMemo = &data.InvoiceMemo{}
	parseTextMemo(description, invoiceMemo)
	if invoiceMemo.Description != "test" {
		t.Fatalf("invoiceMemo.Description should be test, instead got %v", invoiceMemo.Description)
	}
	if invoiceMemo.PayeeName != "payee" {
		t.Fatalf("invoiceMemo.PayeeName should be payee, instead got %v", invoiceMemo.PayeeName)
	}
	if invoiceMemo.PayeeImageURL != "payee_image" {
		t.Fatalf("invoiceMemo.PayeeImageURL should be payee_image, instead got %v", invoiceMemo.PayeeImageURL)
	}
}

func TestWithFullCustomData(t *testing.T) {
	invoiceMemo := &data.InvoiceMemo{
		Description:   "test",
		PayeeName:     "payee",
		PayeeImageURL: "payee_image",
		PayerName:     "payer",
		PayerImageURL: "payer_image",
	}
	description := formatTextMemo(invoiceMemo)
	invoiceMemo = &data.InvoiceMemo{}
	parseTextMemo(description, invoiceMemo)
	if invoiceMemo.Description != "test" {
		t.Fatalf("invoiceMemo.Description should be test, instead got %v", invoiceMemo.Description)
	}
	if invoiceMemo.PayeeName != "payee" {
		t.Fatalf("invoiceMemo.PayeeName should be payee, instead got %v", invoiceMemo.PayeeName)
	}
	if invoiceMemo.PayeeImageURL != "payee_image" {
		t.Fatalf("invoiceMemo.PayeeImageURL should be payee_image, instead got %v", invoiceMemo.PayeeImageURL)
	}
	if invoiceMemo.PayerName != "payer" {
		t.Fatalf("invoiceMemo.PayerName should be payer, instead got %v", invoiceMemo.PayerName)
	}
	if invoiceMemo.PayerImageURL != "payer_image" {
		t.Fatalf("invoiceMemo.PayerImageURL should be payer_image, instead got %v", invoiceMemo.PayerImageURL)
	}
}

func TestShrinkAndExpandInvoice(t *testing.T) {
	// BOLT11 examples boosted from real life debugging and https://github.com/lightningnetwork/lightning-rfc/blob/master/11-payment-encoding.md#examples
	bolt11s := [...]string{
		"lnbc1pvjluezpp5qqqsyqcyq5rqwzqfqqqsyqcyq5rqwzqfqqqsyqcyq5rqwzqfqypqdpl2pkx2ctnv5sxxmmwwd5kgetjypeh2ursdae8g6twvus8g6rfwvs8qun0dfjkxaq8rkx3yf5tcsyz3d73gafnh3cax9rn449d9p5uxz9ezhhypd0elx87sjle52x86fux2ypatgddc6k63n7erqz25le42c4u4ecky03ylcqca784w",
		"lnbc2500u1pvjluezpp5qqqsyqcyq5rqwzqfqqqsyqcyq5rqwzqfqqqsyqcyq5rqwzqfqypqdpquwpc4curk03c9wlrswe78q4eyqc7d8d0xqzpuyk0sg5g70me25alkluzd2x62aysf2pyy8edtjeevuv4p2d5p76r4zkmneet7uvyakky2zr4cusd45tftc9c5fh0nnqpnl2jfll544esqchsrny",
		"lntb20m1pvjluezhp58yjmdan79s6qqdhdzgynm4zwqd5d7xmw5fk98klysy043l2ahrqspp5qqqsyqcyq5rqwzqfqqqsyqcyq5rqwzqfqqqsyqcyq5rqwzqfqypqfpp3x9et2e20v6pu37c5d9vax37wxq72un98kmzzhznpurw9sgl2v0nklu2g4d0keph5t7tj9tcqd8rexnd07ux4uv2cjvcqwaxgj7v4uwn5wmypjd5n69z2xm3xgksg28nwht7f6zspwp3f9t",
		"lnbc20m1pvjluezpp5qqqsyqcyq5rqwzqfqqqsyqcyq5rqwzqfqqqsyqcyq5rqwzqfqypqhp58yjmdan79s6qqdhdzgynm4zwqd5d7xmw5fk98klysy043l2ahrqsfpp3qjmp7lwpagxun9pygexvgpjdc4jdj85fr9yq20q82gphp2nflc7jtzrcazrra7wwgzxqc8u7754cdlpfrmccae92qgzqvzq2ps8pqqqqqqpqqqqq9qqqvpeuqafqxu92d8lr6fvg0r5gv0heeeqgcrqlnm6jhphu9y00rrhy4grqszsvpcgpy9qqqqqqgqqqqq7qqzqj9n4evl6mr5aj9f58zp6fyjzup6ywn3x6sk8akg5v4tgn2q8g4fhx05wf6juaxu9760yp46454gpg5mtzgerlzezqcqvjnhjh8z3g2qqdhhwkj",
		"lnbc20m1pvjluezhp58yjmdan79s6qqdhdzgynm4zwqd5d7xmw5fk98klysy043l2ahrqspp5qqqsyqcyq5rqwzqfqqqsyqcyq5rqwzqfqqqsyqcyq5rqwzqfqypqfppj3a24vwu6r8ejrss3axul8rxldph2q7z9kmrgvr7xlaqm47apw3d48zm203kzcq357a4ls9al2ea73r8jcceyjtya6fu5wzzpe50zrge6ulk4nvjcpxlekvmxl6qcs9j3tz0469gq5g658y",
		"lnbc20m1pvjluezhp58yjmdan79s6qqdhdzgynm4zwqd5d7xmw5fk98klysy043l2ahrqspp5qqqsyqcyq5rqwzqfqqqsyqcyq5rqwzqfqqqsyqcyq5rqwzqfqypqfppqw508d6qejxtdg4y5r3zarvary0c5xw7kepvrhrm9s57hejg0p662ur5j5cr03890fa7k2pypgttmh4897d3raaq85a293e9jpuqwl0rnfuwzam7yr8e690nd2ypcq9hlkdwdvycqa0qza8",
	}

	for _, bolt11 := range bolt11s {
		shrunkInvoice, err := shrinkInvoice(bolt11)
		if err != nil {
			t.Fatalf("Bug in shrinkInvoice: %v", err)
		}

		expandedInvoice, err := ExpandInvoice(shrunkInvoice)
		if err != nil {
			t.Fatalf("Bug in expandedInvoice: %v", err)
		}

		if expandedInvoice != bolt11 {
			t.Fatalf("Original and expanded BOLT-11 string not the same!")
		}

		fmt.Printf("Shrunk invoice %f percent smaller!\n", (1-((float32)(len(shrunkInvoice))/(float32)(len(bolt11))))*100)
	}
}
