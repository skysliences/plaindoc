(function () {
  const rootBody = document.body;
  const dropdownNodes = Array.from(document.querySelectorAll("[data-nav-user-dropdown='1']"))
    .filter((node) => node instanceof HTMLDetailsElement);
  const navToggleButton = document.querySelector("[data-yt-nav-toggle]");
  const searchToggleButton = document.querySelector("[data-yt-search-toggle]");
  const sidebarNode = document.querySelector("[data-yt-sidebar]");
  const sidebarCloseButton = document.querySelector("[data-yt-sidebar-close]");
  const mobileOverlayNode = document.querySelector("[data-yt-mobile-overlay]");
  const searchPanelNode = document.querySelector("[data-yt-search-panel]");
  const mobileViewportMediaQuery = window.matchMedia("(max-width: 900px)");

  let mobileNavOpen = false;
  let mobileSearchOpen = false;

  const setExpanded = (element, expanded) => {
    if (!(element instanceof HTMLElement)) {
      return;
    }
    element.setAttribute("aria-expanded", expanded ? "true" : "false");
  };

  const isMobileViewport = () => mobileViewportMediaQuery.matches;

  const applyMobileUIState = () => {
    const canUseMobileUI = isMobileViewport();
    const shouldOpenNav = canUseMobileUI && mobileNavOpen;
    const shouldOpenSearch = canUseMobileUI && mobileSearchOpen;

    if (rootBody instanceof HTMLElement) {
      rootBody.classList.toggle("yt-mobile-nav-open", shouldOpenNav);
      rootBody.classList.toggle("yt-mobile-search-open", shouldOpenSearch);
    }

    setExpanded(navToggleButton, shouldOpenNav);
    setExpanded(searchToggleButton, shouldOpenSearch);

    if (mobileOverlayNode instanceof HTMLElement) {
      mobileOverlayNode.hidden = !(canUseMobileUI && (shouldOpenNav || shouldOpenSearch));
    }
  };

  const closeMobileUI = () => {
    mobileNavOpen = false;
    mobileSearchOpen = false;
    applyMobileUIState();
  };

  const closeDropdown = (dropdownNode) => {
    if (!(dropdownNode instanceof HTMLDetailsElement)) {
      return;
    }
    dropdownNode.open = false;
  };

  const closeAllDropdowns = () => {
    for (const dropdownNode of dropdownNodes) {
      closeDropdown(dropdownNode);
    }
  };

  const isEventInsideDropdown = (targetNode) => {
    if (!(targetNode instanceof Node)) {
      return false;
    }
    for (const dropdownNode of dropdownNodes) {
      if (dropdownNode.contains(targetNode)) {
        return true;
      }
    }
    return false;
  };

  document.addEventListener("pointerdown", (event) => {
    if (isEventInsideDropdown(event.target)) {
      return;
    }
    closeAllDropdowns();
  }, true);

  document.addEventListener("focusin", (event) => {
    if (isEventInsideDropdown(event.target)) {
      return;
    }
    closeAllDropdowns();
  }, true);

  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") {
      return;
    }
    closeMobileUI();
    closeAllDropdowns();
  });

  window.addEventListener("blur", () => {
    closeAllDropdowns();
    closeMobileUI();
  });

  if (navToggleButton instanceof HTMLButtonElement) {
    navToggleButton.addEventListener("click", () => {
      if (!isMobileViewport()) {
        return;
      }
      mobileNavOpen = !mobileNavOpen;
      if (mobileNavOpen) {
        mobileSearchOpen = false;
      }
      applyMobileUIState();
    });
  }

  if (searchToggleButton instanceof HTMLButtonElement) {
    searchToggleButton.addEventListener("click", () => {
      if (!isMobileViewport()) {
        return;
      }
      mobileSearchOpen = !mobileSearchOpen;
      if (mobileSearchOpen) {
        mobileNavOpen = false;
      }
      applyMobileUIState();
      if (mobileSearchOpen && searchPanelNode instanceof HTMLElement) {
        const inputNode = searchPanelNode.querySelector(".yt-header-search-input");
        if (inputNode instanceof HTMLInputElement) {
          inputNode.focus();
          inputNode.select();
        }
      }
    });
  }

  if (sidebarCloseButton instanceof HTMLButtonElement) {
    sidebarCloseButton.addEventListener("click", () => {
      mobileNavOpen = false;
      applyMobileUIState();
    });
  }

  if (mobileOverlayNode instanceof HTMLElement) {
    mobileOverlayNode.addEventListener("click", () => {
      closeMobileUI();
    });
  }

  if (sidebarNode instanceof HTMLElement) {
    sidebarNode.addEventListener("click", (event) => {
      if (!isMobileViewport()) {
        return;
      }
      const targetNode = event.target;
      if (!(targetNode instanceof Element)) {
        return;
      }
      if (targetNode.closest(".yt-sidebar-item")) {
        mobileNavOpen = false;
        applyMobileUIState();
      }
    });
  }

  if (mobileViewportMediaQuery && typeof mobileViewportMediaQuery.addEventListener === "function") {
    mobileViewportMediaQuery.addEventListener("change", () => {
      if (!isMobileViewport()) {
        closeMobileUI();
        return;
      }
      applyMobileUIState();
    });
  } else {
    window.addEventListener("resize", () => {
      if (!isMobileViewport()) {
        closeMobileUI();
      }
    });
  }

  const createSpaceDialog = document.querySelector("[data-create-space-dialog]");
  if (createSpaceDialog instanceof HTMLDialogElement) {
    const form = createSpaceDialog.querySelector("form");
    const nameInput = createSpaceDialog.querySelector("input[name='name']");
    const submit = form.querySelector("button[type='submit']");
    for (const trigger of document.querySelectorAll("[data-create-space-trigger]")) {
      trigger.addEventListener("click", () => {
        closeAllDropdowns();
        closeMobileUI();
        createSpaceDialog.showModal();
      });
    }
    createSpaceDialog.querySelector("[data-create-space-cancel]").addEventListener("click", () => {
      createSpaceDialog.close();
    });
    nameInput.addEventListener("input", () => nameInput.setCustomValidity(""));
    form.addEventListener("submit", (event) => {
      const name = nameInput.value.trim();
      if (name === "" || Array.from(name).length > 64) {
        event.preventDefault();
        nameInput.setCustomValidity("请输入 1 至 64 个字符的空间名称。");
        nameInput.reportValidity();
        return;
      }
      nameInput.value = name;
      submit.disabled = true;
    });
    window.addEventListener("pageshow", () => {
      submit.disabled = false;
    });
    if (createSpaceDialog.hasAttribute("data-reopen")) {
      createSpaceDialog.showModal();
      const url = new URL(window.location.href);
      url.searchParams.delete("spaceCreationError");
      window.history.replaceState(null, "", url);
    }
  }

  const managementDialog = document.querySelector("#space-management-dialog");
  if (managementDialog instanceof HTMLDialogElement) {
    const frame = managementDialog.querySelector("iframe");
    let activeTrigger = null;
    for (const trigger of document.querySelectorAll("[data-space-manage-url]")) {
      trigger.addEventListener("click", () => {
        activeTrigger = trigger;
        frame.src = trigger.dataset.spaceManageUrl;
        managementDialog.showModal();
      });
    }
    managementDialog.addEventListener("close", () => {
      frame.removeAttribute("src");
      activeTrigger = null;
    });
    window.addEventListener("message", (event) => {
      if (!managementDialog.open || !activeTrigger || event.source !== frame.contentWindow) return;
      const frameURL = new URL(activeTrigger.dataset.spaceManageUrl, window.location.href);
      if (event.origin !== frameURL.origin) return;
      if (event.data?.type === "plaindoc:close-space-management") {
        managementDialog.close();
      }
      if (event.data?.type === "plaindoc:space-renamed" &&
          event.data.spaceId === frameURL.searchParams.get("space") &&
          typeof event.data.name === "string") {
        const card = activeTrigger.closest(".yt-card");
        card.querySelector(".yt-card-title").textContent = event.data.name;
        card.querySelector(".yt-card-title-link").setAttribute("aria-label", `打开空间 ${event.data.name}`);
        card.querySelector(".yt-card-thumb").setAttribute("aria-label", `进入空间 ${event.data.name}`);
        card.querySelector(".yt-card-image").alt = `${event.data.name} 封面`;
        activeTrigger.setAttribute("aria-label", `管理空间 ${event.data.name}`);
      }
    });
  }

  applyMobileUIState();
})();
