import logo from "./assets/klap-logo.svg";
import home from "./assets/tossface/home.svg";
import check from "./assets/tossface/check.svg";
import play from "./assets/tossface/play.svg";
import bell from "./assets/tossface/bell.svg";
import grid from "./assets/tossface/grid.svg";
import chart from "./assets/tossface/chart.svg";
import calendar from "./assets/tossface/calendar.svg";
import activity from "./assets/tossface/activity.svg";
import settings from "./assets/tossface/settings.svg";
import search from "./assets/tossface/search.svg";
import arrow from "./assets/tossface/arrow.svg";
import refresh from "./assets/tossface/refresh.svg";
import close from "./assets/tossface/close.svg";
import done from "./assets/tossface/done.svg";
import campus from "./assets/tossface/campus.svg";
import license from "./assets/tossface/LICENSE.txt";

const icons: Record<string, string> = {
  home, check, play, bell, grid, chart, calendar, activity, settings,
  search, arrow, refresh, close, done, campus,
};

export function Icon({ name }: { name: string }) {
  return <img className={`icon icon-${name}`} src={icons[name] || home} alt="" aria-hidden="true" draggable={false} />;
}

export function BrandLogo() {
  return <img className="brand-logo" src={logo} alt="" aria-hidden="true" draggable={false} />;
}

export function AssetCredits() {
  return (
    <details className="asset-credits">
      <summary>디자인 에셋 안내</summary>
      <div>
        <p>토스페이스(Tossface) · 토스팀(비바리퍼블리카) 제공 · v1.6.1</p>
        <p><a href="https://github.com/toss/tossface" target="_blank" rel="noreferrer">원본 저장소</a> · <a href="https://toss.im/tossface/copyright" target="_blank" rel="noreferrer">저작권 안내</a></p>
        <pre>{license}</pre>
      </div>
    </details>
  );
}
