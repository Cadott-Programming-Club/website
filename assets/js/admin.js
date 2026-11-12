document.addEventListener("click", async (event) => {
	const button = event.target.closest("[data-copy]");
	if (!button) return;
	const source = document.getElementById(button.dataset.copy);
	const text = source.value ?? source.textContent;
	try {
		await navigator.clipboard.writeText(text);
	} catch {
		source.select?.();
		document.execCommand("copy");
	}
	const label = button.querySelector("[data-label]");
	const before = label.textContent;
	label.textContent = "Copied";
	setTimeout(() => (label.textContent = before), 1500);
});

document.addEventListener("input", (event) => {
	if (!event.target.matches("[data-present]")) return;
	const count = document.querySelectorAll("[data-present]:checked").length;
	document.querySelectorAll("[data-count]").forEach((el) => (el.textContent = count));
});

document.addEventListener("submit", (event) => {
	const message = event.target.dataset.confirm;
	if (message && !window.confirm(message)) event.preventDefault();
});

document.addEventListener("click", (event) => {
	const add = event.target.closest("[data-new-add]");
	if (!add) return;
	const form = add.closest("form");
	const row = form.querySelector("[data-new-template]").content.firstElementChild.cloneNode(true);
	form.querySelector("[data-new-list]").append(row);
	row.querySelector("input").focus();
});

(() => {
	const form = document.getElementById("attendance");
	if (!form) {
		if (/\/admin\/meetings\/\d+\/email/.test(location.pathname)) {
			try { Object.keys(localStorage).filter((k) => k.startsWith("cpc-draft:")).forEach((k) => localStorage.removeItem(k)); } catch {}
		}
		return;
	}
	const key = "cpc-draft:" + form.getAttribute("action");
	const store = {
		get() { try { return JSON.parse(localStorage.getItem(key)); } catch { return null; } },
		set(v) { try { localStorage.setItem(key, JSON.stringify(v)); } catch {} },
	};
	const list = form.querySelector("[data-new-list]");
	const addRow = () => {
		const row = form.querySelector("[data-new-template]").content.firstElementChild.cloneNode(true);
		list.append(row);
		return row;
	};
	const save = () => {
		const data = { present: [], fields: {}, rows: [] };
		form.querySelectorAll("[data-present]:checked").forEach((el) => data.present.push(el.value));
		form.querySelectorAll("input[name]:not([type=checkbox]):not([name^=new_]), textarea[name], select[name]:not([name^=new_])").forEach((el) => (data.fields[el.name] = el.value));
		list.querySelectorAll("[name=new_first]").forEach((first) => {
			const row = first.parentElement;
			data.rows.push([first.value, row.querySelector("[name=new_last]").value, row.querySelector("[name=new_grade]").value]);
		});
		store.set(data);
	};
	const data = store.get();
	if (data) {
		form.querySelectorAll("[data-present]").forEach((el) => (el.checked = data.present.includes(el.value)));
		for (const [name, value] of Object.entries(data.fields)) {
			const el = form.elements.namedItem(name);
			if (el && "value" in el) el.value = value;
		}
		data.rows.forEach((values, i) => {
			const firsts = list.querySelectorAll("[name=new_first]");
			const row = firsts[i] ? firsts[i].parentElement : addRow();
			row.querySelector("[name=new_first]").value = values[0];
			row.querySelector("[name=new_last]").value = values[1];
			row.querySelector("[name=new_grade]").value = values[2];
		});
		const count = form.querySelectorAll("[data-present]:checked").length;
		document.querySelectorAll("[data-count]").forEach((el) => (el.textContent = count));
	}
	form.addEventListener("input", save);
	form.addEventListener("change", save);
	form.addEventListener("click", (e) => { if (e.target.closest("[data-new-add]")) setTimeout(save); });
})();

document.addEventListener("keydown", (event) => {
	const row = event.target.closest?.("[data-cancel]");
	if (!row) return;
	if (event.key === "Escape") htmx.ajax("GET", row.dataset.cancel, { target: row, swap: "outerHTML" });
	if (event.key === "Enter" && event.target.tagName !== "BUTTON") {
		event.preventDefault();
		row.querySelector("[hx-post]").click();
	}
});

document.addEventListener("htmx:afterRequest", () => setTimeout(() => {
	document.querySelectorAll("[data-classroom-list]").forEach((box) => {
		if (!box.querySelector("[data-classroom-item]")) box.remove();
	});
}, 50));
