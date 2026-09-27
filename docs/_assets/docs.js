(function () {
  "use strict";

  const korean = document.documentElement.lang.toLowerCase().startsWith("ko");
  const labels = korean ? {
    skip: "본문으로 건너뛰기", navigation: "문서 탐색", contents: "목차",
    controls: "다이어그램 조작", diagram: "시퀀스 다이어그램", source: "다이어그램 원문 보기",
    in: "확대", out: "축소", fit: "전체 보기", reset: "초기화", zoom: "확대 비율",
    help: "드래그하여 이동하고 스크롤하여 확대·축소합니다. 다이어그램에 초점을 두면 방향키, +, −, Home 키를 사용할 수 있습니다.",
    unavailable: "다이어그램 도구를 불러오지 못했습니다. 원문은 계속 확인할 수 있습니다.",
    failed: "다이어그램을 표시하지 못했습니다. 원문은 계속 확인할 수 있습니다."
  } : {
    skip: "Skip to content", navigation: "Document navigation", contents: "Contents",
    controls: "Diagram controls", diagram: "Sequence diagram", source: "View diagram source",
    in: "Zoom in", out: "Zoom out", fit: "Fit", reset: "Reset", zoom: "Zoom level",
    help: "Drag to pan. Scroll to zoom. Focus the diagram to use arrow keys, +, −, and Home.",
    unavailable: "The diagram library could not be loaded. The original source remains available.",
    failed: "The diagram could not be rendered. The original source remains available."
  };
  const clamp = (value, minimum, maximum) => Math.min(maximum, Math.max(minimum, value));

  document.querySelectorAll("[data-doc-label]").forEach(element => {
    if (labels[element.dataset.docLabel]) element.textContent = labels[element.dataset.docLabel];
  });
  document.querySelectorAll(".doc-sidebar").forEach(element => element.setAttribute("aria-label", labels.navigation));
  document.querySelectorAll(".doc-nav").forEach(element => element.setAttribute("aria-label", labels.contents));

  function markNavigation() {
    document.querySelectorAll(".doc-nav a").forEach(link => {
      const active = link.hash && link.hash === window.location.hash;
      if (active) link.setAttribute("aria-current", "location");
      else link.removeAttribute("aria-current");
    });
  }
  markNavigation();
  window.addEventListener("hashchange", markNavigation);

  function report(viewer, message) {
    const status = viewer.querySelector(".sequence-status");
    if (status) {
      status.textContent = message;
      status.hidden = false;
    }
  }

  function makeSourceDetails(source) {
    const details = document.createElement("details");
    details.className = "sequence-source";
    const summary = document.createElement("summary");
    summary.textContent = labels.source;
    const pre = document.createElement("pre");
    const code = document.createElement("code");
    code.textContent = source;
    pre.append(code);
    details.append(summary, pre);
    return details;
  }

  function setupControls(viewer, viewport, canvas) {
    const toolbar = viewer.querySelector(".sequence-toolbar");
    const zoomLabel = viewer.querySelector(".sequence-zoom");
    const state = { x: 0, y: 0, scale: 1, pointer: null, px: 0, py: 0 };
    const minScale = 0.05;
    const maxScale = 4;

    function update() {
      canvas.style.transform = `translate(${state.x}px, ${state.y}px) scale(${state.scale})`;
      if (zoomLabel) zoomLabel.textContent = `${Math.round(state.scale * 100)}%`;
    }

    function zoomAt(nextScale, clientX, clientY) {
      const bounds = viewport.getBoundingClientRect();
      const x = clientX - bounds.left - viewport.clientLeft;
      const y = clientY - bounds.top - viewport.clientTop;
      const worldX = (x - state.x) / state.scale;
      const worldY = (y - state.y) / state.scale;
      state.scale = clamp(nextScale, minScale, maxScale);
      state.x = x - worldX * state.scale;
      state.y = y - worldY * state.scale;
      update();
    }

    function zoomCenter(factor) {
      const bounds = viewport.getBoundingClientRect();
      zoomAt(state.scale * factor, bounds.left + viewport.clientLeft + viewport.clientWidth / 2,
        bounds.top + viewport.clientTop + viewport.clientHeight / 2);
    }

    function fit() {
      const availableWidth = Math.max(1, viewport.clientWidth - 32);
      const availableHeight = Math.max(1, viewport.clientHeight - 32);
      state.scale = clamp(Math.min(availableWidth / canvas.offsetWidth, availableHeight / canvas.offsetHeight, 1), minScale, 1);
      state.x = (viewport.clientWidth - canvas.offsetWidth * state.scale) / 2;
      state.y = (viewport.clientHeight - canvas.offsetHeight * state.scale) / 2;
      update();
    }

    function reset() {
      state.x = 0;
      state.y = 0;
      state.scale = 1;
      update();
    }

    function endDrag(event) {
      if (state.pointer !== event.pointerId) return;
      state.pointer = null;
      viewport.classList.remove("is-dragging");
      if (viewport.hasPointerCapture(event.pointerId)) viewport.releasePointerCapture(event.pointerId);
    }

    viewport.addEventListener("pointerdown", event => {
      if (event.button !== 0 || !event.isPrimary || state.pointer !== null) return;
      if (event.target.closest("a, button, input, textarea, select")) return;
      state.pointer = event.pointerId;
      state.px = event.clientX;
      state.py = event.clientY;
      viewport.classList.add("is-dragging");
      viewport.focus({ preventScroll: true });
      viewport.setPointerCapture(event.pointerId);
      event.preventDefault();
    });
    viewport.addEventListener("pointermove", event => {
      if (state.pointer !== event.pointerId) return;
      state.x += event.clientX - state.px;
      state.y += event.clientY - state.py;
      state.px = event.clientX;
      state.py = event.clientY;
      update();
    });
    viewport.addEventListener("pointerup", endDrag);
    viewport.addEventListener("pointercancel", endDrag);
    viewport.addEventListener("lostpointercapture", endDrag);
    viewport.addEventListener("wheel", event => {
      event.preventDefault();
      const unit = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? viewport.clientHeight : 1;
      zoomAt(state.scale * Math.exp(-event.deltaY * unit * 0.0015), event.clientX, event.clientY);
    }, { passive: false });
    viewport.addEventListener("keydown", event => {
      if (event.target !== viewport || event.altKey || event.ctrlKey || event.metaKey) return;
      const step = event.shiftKey ? 80 : 32;
      switch (event.key) {
        case "ArrowLeft": state.x -= step; update(); break;
        case "ArrowRight": state.x += step; update(); break;
        case "ArrowUp": state.y -= step; update(); break;
        case "ArrowDown": state.y += step; update(); break;
        case "+": case "=": zoomCenter(1.2); break;
        case "-": case "−": zoomCenter(1 / 1.2); break;
        case "Home": fit(); break;
        default: return;
      }
      event.preventDefault();
    });

    if (toolbar) {
      toolbar.setAttribute("aria-label", labels.controls);
      toolbar.querySelectorAll("button[data-action]").forEach(button => {
        const action = button.dataset.action;
        button.disabled = false;
        button.setAttribute("aria-label", labels[action] || action);
        if (action === "fit" || action === "reset") button.textContent = labels[action];
        button.addEventListener("click", () => {
          if (action === "in") zoomCenter(1.2);
          else if (action === "out") zoomCenter(1 / 1.2);
          else if (action === "fit") fit();
          else if (action === "reset") reset();
        });
      });
      toolbar.hidden = false;
    }
    if (zoomLabel) zoomLabel.setAttribute("aria-label", labels.zoom);
    viewport.setAttribute("aria-label", labels.diagram);
    const help = viewer.querySelector(".sequence-help");
    if (help) { help.textContent = labels.help; help.hidden = false; }
    fit();
  }

  async function renderSequences() {
    const viewers = Array.from(document.querySelectorAll(".sequence-viewer"));
    if (!viewers.length) return;
    if (!window.mermaid || typeof window.mermaid.render !== "function") {
      viewers.forEach(viewer => report(viewer, labels.unavailable));
      return;
    }
    try {
      window.mermaid.initialize({
        startOnLoad: false,
        securityLevel: "strict",
        suppressErrorRendering: true,
        theme: "neutral",
        fontFamily: 'Inter, "Noto Sans KR", system-ui, sans-serif'
      });
    } catch (error) {
      viewers.forEach(viewer => report(viewer, labels.unavailable));
      return;
    }

    for (const [index, viewer] of viewers.entries()) {
      const viewport = viewer.querySelector(".sequence-viewport");
      const canvas = viewer.querySelector(".sequence-canvas");
      const source = canvas && canvas.querySelector("pre.mermaid-source");
      if (!viewport || !canvas || !source) continue;
      const text = source.textContent;
      try {
        const id = `doc-mermaid-${index + 1}`;
        const result = await window.mermaid.render(id, text);
        const rendered = document.createElement("div");
        rendered.innerHTML = result.svg;
        const svg = rendered.querySelector("svg");
        if (!svg) throw new Error("Missing diagram SVG");
        const viewBox = svg.viewBox.baseVal;
        if (viewBox && viewBox.width > 0 && viewBox.height > 0) {
          svg.setAttribute("width", String(viewBox.width));
          svg.setAttribute("height", String(viewBox.height));
        }
        if (!svg.hasAttribute("aria-label") && !svg.hasAttribute("aria-labelledby")) svg.setAttribute("aria-label", labels.diagram);
        svg.setAttribute("role", "img");
        canvas.replaceChildren(svg);
        viewer.append(makeSourceDetails(text));
        viewer.classList.add("is-ready");
        setupControls(viewer, viewport, canvas);
      } catch (error) {
        viewer.classList.remove("is-ready");
        canvas.replaceChildren(source);
        report(viewer, labels.failed);
      }
    }
  }
  renderSequences();
}());
