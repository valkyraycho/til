document.addEventListener("keydown", (e) => {
  const active = document.activeElement && document.activeElement.tagName;
  if (e.key === "/" && active !== "INPUT" && active !== "TEXTAREA") {
    e.preventDefault();
    const box = document.querySelector('input[type="search"]');
    if (box) box.focus();
  }
});
