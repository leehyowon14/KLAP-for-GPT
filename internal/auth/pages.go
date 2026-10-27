package auth

import (
	_ "embed"
	"encoding/base64"
)

// These assets are embedded so setup and OAuth never fetch external resources.
// The logo matches web/src/assets/klap-logo.svg, from KLAP_mac's Icon Composer layers.
//
//go:embed assets/klap-logo.svg
var ownerLogo []byte

//go:embed assets/page.css
var ownerPageCSS string

var ownerLogoURL = "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(ownerLogo)

var pageStyle = `<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" type="image/svg+xml" sizes="any" href="` + ownerLogoURL + `"><style>` + ownerPageCSS + `</style></head><body><main><a class="brand" href="/"><img src="` + ownerLogoURL + `" alt=""><span>KLAP</span></a>`
var loginPage = `<!doctype html><html lang="ko"><head><title>KLAP 연결</title>` + pageStyle + `<p class="eyebrow">나의 캠퍼스, 한곳에서</p><h1>{{if .Setup}}KLAP을 시작해 볼까요?{{else}}다시 만나 반가워요{{end}}</h1>{{if .Setup}}<p>설치 토큰을 확인하고, 이 개인 인스턴스에서 사용할 소유자 암호를 설정해 주세요.</p><form method="post" action="/setup"><label>설치 토큰<input name="bootstrap" type="password" required autocomplete="off"></label>{{else}}<p>소유자 암호를 입력하면 나의 학습 공간으로 이어집니다.</p><form method="post" action="/login">{{end}}<input name="next" type="hidden" value="{{.Next}}"><label>소유자 암호<input name="password" type="password" required minlength="12" maxlength="72" autocomplete="{{if .Setup}}new-password{{else}}current-password{{end}}"></label><button>계속하기</button></form>{{if .Setup}}<p class="footnote">설치 토큰은 데이터 폴더의 bootstrap.token에 있어요.</p>{{end}}</main></body></html>`
var consentPage = `<!doctype html><html lang="ko"><head><title>KLAP 연결 허용</title>` + pageStyle + `<p class="eyebrow">나의 캠퍼스와 연결</p><h1>KLAP을 연결할까요?</h1><p>이 클라이언트가 수업 정보를 조회하고, 요청한 수강·동기화 작업과 새 콘텐츠 모니터링을 실행할 수 있습니다.</p><small>클라이언트: {{.Client}}<br>돌아갈 주소: {{.Redirect}}</small><form method="post" action="{{.Action}}"><input type="hidden" name="csrf" value="{{.CSRF}}"><div class="actions"><button class="secondary" name="decision" value="deny">취소</button><button name="decision" value="allow">연결 허용</button></div></form></main></body></html>`
