package main

import (
	"strings"
	"testing"
)

// 短信存储可视化条 mock:组合命令与单命令都应返回与真机一致的 +CPMS/+CSCA 形状。
func TestMockSMSStorageResponse(t *testing.T) {
	composite := `AT+CPMS="SM";+CPMS?;+CPMS="ME";+CPMS?;+CSCA?`
	resp := mockATResponse(composite)
	for _, want := range []string{
		`+CPMS: "SM",0,40,"ME",2,255,"ME",2,255`,
		`+CPMS: "ME",2,255,"ME",2,255,"ME",2,255`,
		`+CSCA: "002B0038003600310033003800300030003200310030003500300030",145`,
	} {
		if !strings.Contains(resp, want) {
			t.Fatalf("mockATResponse(%q) missing %q, got %q", composite, want, resp)
		}
	}
	if !strings.HasSuffix(resp, "OK\r\n") {
		t.Fatalf("mockATResponse(%q) should end with OK, got %q", composite, resp)
	}

	// 单独 AT+CPMS?:mem1 缺省 ME,返回三个带名三元组。
	if resp := mockATResponse(`AT+CPMS?`); !strings.Contains(resp, `+CPMS: "ME",2,255`) {
		t.Fatalf("mockATResponse(AT+CPMS?) = %q, want contain ME triplet", resp)
	}

	// 单独 AT+CSCA?:返回 UCS2 hex 的 SMSC 行。
	if resp := mockATResponse(`AT+CSCA?`); !strings.Contains(resp, "+CSCA: ") {
		t.Fatalf("mockATResponse(AT+CSCA?) = %q, want contain +CSCA line", resp)
	}
}

// 分流保护:CMGD/CMGS/CMGR 组合即使前置 +CPMS 也不进存储分支;CMGL 组合仍走列表分支。
func TestMockSMSStorageBranchExclusions(t *testing.T) {
	deleteCombo := `AT+CPMS="ME","ME","ME";+CMGD=3`
	if resp := mockATResponse(deleteCombo); strings.Contains(resp, "+CPMS: ") {
		t.Fatalf("mockATResponse(%q) = %q, want default echo without +CPMS lines", deleteCombo, resp)
	}

	listCombo := `AT+CSMS=1;+CSDH=0;+CNMI=2,1,0,0,0;+CMGF=0;+CPMS="ME","ME","ME";+CMGL=4`
	if resp := mockATResponse(listCombo); !strings.Contains(resp, "+CMGL: 1") {
		t.Fatalf("mockATResponse(%q) = %q, want CMGL branch payload", listCombo, resp)
	}
}
