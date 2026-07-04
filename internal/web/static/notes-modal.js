document.addEventListener("click", function (e) {
  const btn = e.target.closest(".notes-btn");
  if (!btn) return;
  const dialog = document.getElementById("notes-dialog");
  if (!dialog) return;
  dialog.querySelector(".notes-dialog-subject").textContent = btn.dataset.subject || "";
  dialog.querySelector(".notes-dialog-body").textContent = btn.dataset.notes || "";
  dialog.showModal();
});

document.addEventListener("click", function (e) {
  if (e.target.id === "notes-dialog") e.target.close();
});
