package nativeengine_test

import (
	"context"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestNativeHTMLMediaElementAndAudioConstructor(t *testing.T) {
	t.Parallel()

	page, location := newCanvasTestPage(t, `<!doctype html><html><body><audio id="a" muted controls></audio><video id="v" loop autoplay></video></body></html>`)
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
		let settled = false;
		const audio = document.getElementById("a");
		const video = document.getElementById("v");
		const constructed = new Audio("sound.mp3");
		if (!(audio instanceof HTMLMediaElement) || !(video instanceof HTMLMediaElement) || !(constructed instanceof HTMLMediaElement)) {
			throw new Error("media prototype chain mismatch");
		}
		if (constructed.tagName !== "AUDIO" || constructed.getAttribute("preload") !== "auto") {
			throw new Error("Audio() constructor metadata mismatch: " + constructed.tagName + "/" + constructed.getAttribute("preload"));
		}
		if (constructed.src !== "https://gossamer.test/canvas/sound.mp3" || constructed.currentSrc !== "sound.mp3") {
			throw new Error("Audio(src) mismatch: " + constructed.src + " / " + constructed.currentSrc);
		}
		if (audio.paused !== true || audio.muted !== true || audio.controls !== true || audio.volume !== 1 || audio.playbackRate !== 1) {
			throw new Error("initial audio defaults mismatch");
		}
		if (video.loop !== true || video.autoplay !== true || video.ended !== false || !Number.isNaN(video.duration)) {
			throw new Error("initial video attributes mismatch");
		}
		if (audio.readyState !== 0 || audio.networkState !== 0 || audio.canPlayType("audio/mpeg") !== "") {
			throw new Error("media readiness/canPlayType mismatch");
		}

		audio.muted = false;
		audio.volume = 0.25;
		audio.currentTime = 12.5;
		audio.playbackRate = 1.5;
		audio.controls = false;
		if (audio.muted !== false || audio.hasAttribute("muted") !== true) {
			// muted IDL setter only toggles state without mutating attribute
			throw new Error("unexpected muted IDL state");
		}
		if (audio.volume !== 0.25 || audio.currentTime !== 12.5 || audio.playbackRate !== 1.5 || audio.controls !== false) {
			throw new Error("updated audio state mismatch");
		}

		let threwRange = false;
		try {
			audio.volume = 1.5;
		} catch (e) {
			threwRange = true;
		}
		if (!threwRange) {
			throw new Error("expected out-of-range volume assignment to throw");
		}

		let threwType = false;
		try {
			HTMLMediaElement.prototype.pause.call(document.createElement("div"));
		} catch (e) {
			threwType = e instanceof TypeError;
		}
		if (!threwType) {
			throw new Error("expected non-media receiver pause() to throw TypeError");
		}

		const playPromise = audio.play();
		if (audio.paused !== false) {
			throw new Error("expected play() to clear paused");
		}
		playPromise.then(() => {
			audio.pause();
			if (audio.paused !== true) {
				throw new Error("expected pause() to set paused");
			}
			audio.load();
			if (audio.currentTime !== 0 || audio.paused !== true) {
				throw new Error("expected load() to reset currentTime and paused");
			}
			settled = true;
		});
		window.__mediaSettled = () => settled;
	`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
		if (!window.__mediaSettled()) {
			throw new Error("expected play() promise callback to settle cleanly");
		}
	`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}
