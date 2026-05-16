import "../styles/boot-splash.css";
import bootLogoUrl from "../assets/images/app_logo_alt.svg";

const bootSplashMarkup = `
  <div class="boot-splash" role="status" aria-live="polite" aria-label="Loading application" aria-busy="true">
    <div class="boot-splash__center" aria-hidden="true">
      <span class="boot-splash__mark">
        <img class="boot-splash__logo" src="${bootLogoUrl}" alt="" role="presentation" />
      </span>
      <span class="boot-splash__signal">
        <span class="boot-splash__signal-bar"></span>
      </span>
    </div>
  </div>
`;

function mountBootSplash(target) {
  if (!target) {
    return;
  }
  target.innerHTML = bootSplashMarkup;
}

function dismissBootSplash(target) {
  if (!target) {
    return Promise.resolve();
  }
  const splash = target.firstElementChild;
  if (!splash) {
    target.innerHTML = "";
    return Promise.resolve();
  }
  return new Promise((resolve) => {
    let settled = false;
    const finish = () => {
      if (settled) {
        return;
      }
      settled = true;
      target.innerHTML = "";
      resolve();
    };
    splash.addEventListener("transitionend", finish, { once: true });
    splash.classList.add("is-exiting");
    window.setTimeout(finish, 320);
  });
}

const BootSplash = {
  name: "BootSplash",
  template: bootSplashMarkup,
};

export { bootSplashMarkup, mountBootSplash, dismissBootSplash };

export default BootSplash;
