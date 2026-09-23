"use strict";
// One presentation mapper shared by every screen. Domain decisions stay in Go.
const statuses = Object.assign(Object.create(null), {
  eligible: ["Можно подготовить follow-up", "green"],
  too_early: ["Пока рано", ""],
  not_eligible: ["Follow-up не предлагается", ""],
  conversation_closed: ["Разговор завершён", ""],
  discovered: ["Найдена", ""],
  analyzed: ["Проанализирована", "blue"],
  shortlisted: ["В избранном", "blue"],
  applied: ["Отклик отправлен", "blue"],
  employer_replied: ["Работодатель ответил", "green"],
  candidate_action_required: ["Нужен ваш ответ", "amber"],
  waiting_employer: ["Ждём работодателя", ""],
  interview: ["Собеседование", "blue"],
  offer: ["Оффер", "green"],
  rejected: ["Отклонено", "red"],
  archived: ["Архив", ""],
  closed: ["Закрыто", ""],
  unknown: ["Неизвестно", ""],
  apply: ["Apply", "green"],
  maybe: ["Maybe", "amber"],
  skip: ["Skip", ""],
  generated: ["Draft · generated", "blue"],
  approved: ["Одобрен", "green"],
  superseded: ["Заменён", ""],
  sent: ["Отправлен ранее", ""],
  sent_unconfirmed: ["Отправлен, доставка не подтверждена", "amber"],
  delivery_confirmed: ["Доставка подтверждена", "green"],
  delivery_uncertain: ["Доставка не определена", "amber"],
  pending: ["Ожидает решения", "amber"],
  answered: ["Ответ сохранён", "green"],
  dismissed: ["Закрыто", ""],
  needs_confirmation: ["Нужно подтверждение", "amber"],
  confirmed: ["Подтверждено", "green"],
  verified: ["Проверено", "green"],
  hypothesis: ["Гипотеза", "amber"],
  draft_reply: ["Черновик готов", "blue"],
  need_candidate_input: ["Нужно уточнение", "amber"],
  manual_review: ["Ручная проверка", "amber"],
  no_reply_needed: ["Ответ не требуется", ""],
  waiting_candidate_reply: ["Ответить работодателю", "amber"],
  waiting_employer_reply: ["Ждём работодателя", ""],
  prepare_interview: ["Подготовиться к интервью", "blue"],
  follow_up_possible: ["Возможен follow-up", ""],
  created: ["Создано", ""],
  matched: ["Оценено", "blue"],
  message_received: ["Сообщение работодателя", "green"],
  interview_scheduled: ["Назначено интервью", "blue"],
  status_changed: ["Изменён статус", ""],
  offer_received: ["Получен оффер", "green"],
  rejected_knowledge: ["Отклонено", "red"],
  ready_manual_reply: ["Ready for manual reply", "green"],
  NEEDS_REPLY: ["Нужно ответить", "amber"],
  NEEDS_USER_ACTION: ["Нужно сделать", "amber"],
  WAITING_FOR_EMPLOYER: ["Ждём работодателя", ""],
  NO_REPLY_NEEDED: ["Не требует действий", ""],
  INTERVIEW: ["Интервью", "blue"],
  EXTERNAL_ACTION: ["Внешнее действие", "blue"],
  NEEDS_CLARIFICATION: ["Нужно уточнение", "amber"],
  TERMINAL: ["Завершено", ""],
  recommended: ["Recommended for pilot", "green"],
  possible: ["Possible", "amber"],
  not_recommended: ["Not recommended", ""],
  review_required: ["Нужна проверка", "amber"],
  ineligible: ["Не подходит", "red"],
  unavailable: ["Недоступна", ""],
  compatible: ["Совместима", "green"],
  stretch: ["Stretch / запас", "amber"],
  unlikely: ["Слабое совпадение", ""],
  hard_incompatible: ["Жёсткий конфликт", "red"],
  high: ["Высокая уверенность", "green"],
  medium: ["Средняя уверенность", "amber"],
  low: ["Низкая уверенность", "amber"],
  complete: ["Данные полнее", "blue"],
  partial: ["Данные частичные", "amber"],
  insufficient_evidence: ["Недостаточно данных", "amber"],
  to_review: ["К просмотру", "blue"],
  worth_another_look: ["Вернуться позже", "amber"],
  stretch_manual_review: ["Stretch / ручная проверка", "amber"],
  closed_excluded: ["Закрыта / исключена", ""],
  unseen: ["Не просмотрена", "blue"],
  interesting: ["Интересна", "green"],
});
const titles = {
  "/": "Overview",
  "/today": "Today",
  "/inbox": "Inbox",
  "/applications": "Applications",
  "/vacancies": "Vacancies",
  "/career": "Career Agent",
  "/knowledge": "Knowledge",
  "/knowledge/questions": "Knowledge questions",
  "/analytics": "Analytics",
  "/sync": "Maintenance",
  "/health": "System Health",
  "/reliability": "Reliability",
};
const main = document.getElementById("content");
let syncWatching = false, lastGeneration = null;
let busy = false,
  loading = false,
  currentRequest = 0;
const draftsForCopy = new Map();
const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const list = (value) => (Array.isArray(value) ? value : []);
const badge = (value) => {
  const [label, color] = statuses[value] || [value || "Неизвестно", ""];
  return `<span class="badge ${color}">${esc(label)}</span>`;
};
const date = (value) =>
  !value || String(value).startsWith("0001-")
    ? "—"
    : new Date(value).toLocaleString("ru-RU", {
        day: "2-digit",
        month: "short",
        year: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      });
const shortDate = (value) =>
  !value || String(value).startsWith("0001-")
    ? "—"
    : new Date(value).toLocaleDateString("ru-RU", {
        day: "2-digit",
        month: "short",
      });
const score = (m) =>
  m
    ? `<span class="score">${esc(m.score)}<span class="muted"> / 100</span></span>`
    : '<span class="muted">Не оценено</span>';
const urlID = (id) => encodeURIComponent(String(id));
const link = (path, label) => `<a href="${esc(path)}">${esc(label)}</a>`;
const empty = (
  title = "Пока здесь пусто",
  description = "Синхронизируйте данные HH, чтобы наполнить workspace.",
  action = '<a class="secondary-link" href="/sync">Открыть Sync →</a>',
) =>
  `<div class="empty"><div class="empty-symbol">◇</div><strong>${esc(title)}</strong><p>${esc(description)}</p>${action}</div>`;
const panel = (title, body, action = "") =>
  `<section class="panel"><div class="panel-header"><h2>${esc(title)}</h2>${action}</div>${body}</section>`;
const ul = (items) =>
  list(items).length
    ? `<ul class="list">${items.map((v) => `<li>${esc(typeof v === "string" ? v : v.question || v.message || v.description || v.text || v.reason || "")}</li>`).join("")}</ul>`
    : '<p class="muted">Нет сохранённых данных</p>';
const heading = (title, description, actions = "") =>
  `<div class="page-heading"><div><div class="eyebrow">YOUR CAREER WORKSPACE</div><h1>${esc(title)}</h1><div class="subtitle">${esc(description)}</div></div><div class="actions">${actions}</div></div>`;
const pairs = (values) =>
  `<dl class="facts">${values.map(([key, value]) => `<div><dt>${esc(key)}</dt><dd>${esc(value || "Не указано")}</dd></div>`).join("")}</dl>`;
function safeHHLink(raw) {
  try {
    const u = new URL(raw);
    if (
      u.protocol === "https:" &&
      (u.hostname === "hh.ru" || u.hostname.endsWith(".hh.ru")) &&
      !u.username &&
      !u.password
    )
      return `<a class="secondary-link" href="${esc(u.href)}" target="_blank" rel="noopener noreferrer">Открыть на HH ↗</a>`;
  } catch {}
  return "";
}
function safeExternalLink(raw) {
  try {
    const u = new URL(raw);
    if (u.protocol === "https:" && !u.username && !u.password)
      return `<a class="secondary-link" href="${esc(u.href)}" target="_blank" rel="noopener noreferrer">Открыть ссылку ↗</a>`;
  } catch {}
  return "";
}
async function api(path, body, asyncSync = false) {
  const response = await fetch(
    `/api${path}`,
    body === undefined
      ? { cache: "no-store" }
      : {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Career-Agent": "local",
            ...(asyncSync ? { Prefer: "respond-async" } : {}),
          },
          body: JSON.stringify(body),
        },
  );
  let data;
  try {
    data = await response.json();
  } catch {
    data = { error: `HTTP ${response.status}` };
  }
  if (!response.ok) {
    const failure = new Error(data.error || "Не удалось выполнить запрос");
    failure.code = data.code || "HTTP_ERROR";
    failure.reason = data.reason || data.error || `HTTP ${response.status}`;
    failure.status = response.status;
    failure.result = data.result;
    throw failure;
  }
  return data;
}
function sendFailureMessage(error) {
  if (error?.code === "BLOCKED_BY_DRY_RUN") {
    return "Send blocked before HH request\nReason: HH_DRY_RUN=true";
  }
  const reason = error?.reason || error?.message || "unknown send failure";
  return `${error?.message || "Send failed before HH request"}\nReason: ${reason}`;
}
async function updateNotificationIndicator(dashboardPromise) {
  try {
    const d = await dashboardPromise;
    const indicator = document.getElementById("inbox-count");
    if (indicator) indicator.textContent = d.metrics?.workflow_important || "";
  } catch {}
}
async function updateGlobalSyncStatus() {
  const node = document.getElementById("global-sync-status");
  if (!node) return;
  try {
    const d = await api("/sync/status");
    const p = list(d.progress)[0];
    if (!d.running) {
      node.textContent = d.state?.last_success ? `HH: Up to date · ${date(d.state.last_success)}` : "HH: Local data available";
      node.className = "sync-status";
      return;
    }
    const target = p?.target?.startsWith("conversation:") ? "targeted conversation" : p?.target === "inbox" ? "inbox" : p?.target === "conversations" ? "Full verification" : (p?.target || "refresh");
    const progress = p ? ` · ${p.processed}/${p.fetched}` : "";
    node.textContent = `HH: ${target} running${progress}`;
    node.className = "sync-status active";
  } catch {
    node.textContent = "HH: unavailable";
    node.className = "sync-status warning";
  }
}
function toast(text) {
  const box = document.getElementById("toast");
  box.textContent = text;
  box.hidden = false;
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => (box.hidden = true), 6500);
}
function metricsCards(m) {
  const cards = [
    [
      "Vacancies",
      m.total_vacancies,
      `${m.analyzed_vacancies} проанализировано`,
      "▦",
    ],
    ["Follow-up available", m.follow_up_eligible ?? 0, "Готовность к подготовке draft", "◷"],
    ["Applications", m.applications, "Сохранённые отклики", "↗"],
    ["Employer replies", m.employer_replies, "Отклики с ответом", "▤"],
    [
      "Need my response",
      m.need_my_response,
      "Диалоги, требующие действия",
      "↳",
    ],
    [
      "Waiting employer",
      m.waiting_employer,
      "Следующий шаг — за работодателем",
      "◷",
    ],
    ["Interviews", m.interviews, "Сохранённые собеседования", "◈"],
    ["Offers", m.offers, "Полученные предложения", "◇"],
    ["Rejected", m.rejected, "Сохранённые отказы", "−"],
  ];
  return `<div class="stats">${cards.map(([label, value, note, icon], i) => `<div class="stat ${i === 3 ? "featured" : ""}"><div class="stat-label">${label}<span class="stat-icon">${icon}</span></div><div class="stat-value">${value}</div><div class="stat-note">${esc(note)}</div></div>`).join("")}</div>`;
}
function inboxCard(item) {
  const c = item.conversation;
  const workflow = item.workflow || {};
  const draft = list(item.ai_drafts).find((value) => value.status === "generated");
  const preview = draft ? `<div class="inbox-preview"><strong>AI draft:</strong> ${esc(draft.text)}</div>` : "";
  const action = workflow.what_to_do ? `<p class="muted truncate">${esc(workflow.what_to_do)}</p>` : "";
  const feedbackStates = [["NEEDS_REPLY", "Нужно ответить"], ["NEEDS_USER_ACTION", "Нужно действие"], ["WAITING_FOR_EMPLOYER", "Ждём работодателя"], ["INTERVIEW", "Интервью"], ["EXTERNAL_ACTION", "Внешнее действие"], ["NO_REPLY_NEEDED", "Не требует ответа"], ["TERMINAL", "Terminal"], ["other", "Другое"]];
  const feedback = `<div class="quality-actions"><span class="muted">Classification:</span><button data-action="classification-correct" data-id="${esc(c.id)}">Верно</button><select data-classification-state="${esc(c.id)}" aria-label="Ожидаемое состояние">${feedbackStates.map(([value, label]) => `<option value="${value}">${label}</option>`).join("")}</select><button data-action="classification-wrong" data-id="${esc(c.id)}">Неверное состояние</button><button data-action="conversation-irrelevant" data-id="${esc(c.id)}">Не требует внимания</button></div>`;
  return `<a class="queue-row workflow-row" href="/conversations/${urlID(c.id)}"><span class="avatar">${esc((c.company_name || "?").slice(0, 2).toUpperCase())}</span><div class="row-main"><div class="row-title"><strong>${esc(c.company_name || "Компания не указана")}</strong>${badge(item.bucket || workflow.state || "")}</div><p class="truncate">${esc(c.vacancy_title)}</p><p class="truncate">${esc(item.latest_message?.text || workflow.what_is_happening || "История ещё не загружена")}</p>${action}${preview}<p>${date(item.latest_message?.timestamp || c.updated_at)}</p>${feedback}</div></a>`;
}

function workflowSectionCard(section, items) {
  const body = items.length
    ? items.map(inboxCard).join("")
    : '<div class="panel-body muted">Сейчас здесь пусто.</div>';
  return `<section class="workflow-section" data-section="${esc(section.id)}"><div class="section-title"><h2>${esc(section.label)}</h2><span class="count-pill">${section.count || 0}</span></div>${body}</section>`;
}

function workflowItemsBySection(items, sections) {
  const map = Object.fromEntries(list(sections).map((section) => [section.id, []]));
  for (const item of items) {
    const state = item.workflow?.state;
    const id = ["NEEDS_REPLY"].includes(state)
      ? "needs_reply"
      : ["NEEDS_USER_ACTION", "NEEDS_CLARIFICATION"].includes(state)
        ? "needs_action"
        : ["WAITING_FOR_EMPLOYER"].includes(state)
          ? "waiting"
          : ["INTERVIEW", "EXTERNAL_ACTION"].includes(state)
            ? "interviews"
            : "no_action";
    (map[id] ||= []).push(item);
  }
  return map;
}
function localFreshness(freshness) {
  if (!freshness) return "";
  return `<p class="muted">Local data · ${date(freshness.local_updated_at)} · HH last checked · ${date(freshness.hh_last_checked_at)}</p>`;
}
function applicationTable(rows) {
  if (!rows.length)
    return empty(
      "Отклики не найдены",
      "Попробуйте изменить фильтры или выполните синхронизацию.",
    );
  return `<div class="table-wrap"><table><thead><tr><th>Вакансия / компания</th><th>Match</th><th>Recommendation</th><th>Статус</th><th>Дата отклика</th><th>Последний контакт</th><th>Next action</th></tr></thead><tbody>${rows.map((a) => `<tr><td>${link(`/applications/${urlID(a.id)}`, a.vacancy_title)}<span class="muted">${esc(a.company_name)}</span></td><td>${score(a.match_result)}</td><td>${badge(a.recommendation)}</td><td>${badge(a.display_status)}</td><td class="nowrap">${date(a.applied_at)}</td><td class="nowrap">${date(a.last_contact)}</td><td>${a.next_action ? badge(a.next_action) : "—"}</td></tr>`).join("")}</tbody></table></div>`;
}
function notificationPanel(d) {
  const rows = list(d?.notifications);
  if (!rows.length) return panel("Requires attention", '<div class="panel-body muted">Новых уведомлений нет.</div>');
  return panel("Requires attention", `<div class="panel-body">${rows.slice(0, 8).map(n => `<article class="question"><p><strong>${esc(n.priority || "LOW")} · ${esc(n.lifecycle || "new")}</strong></p><p>${esc(n.message)}</p><p class="muted">${date(n.created_at)}</p><div class="actions">${n.related_conversation_id ? `<a class="secondary-link" data-action="notification-open" data-id="${esc(n.id)}" href="/conversations/${urlID(n.related_conversation_id)}">Открыть диалог →</a>` : ""}<button data-action="notification-ack" data-id="${esc(n.id)}">Dismiss</button><button data-action="notification-snooze" data-id="${esc(n.id)}">Remind tomorrow</button></div></article>`).join("")}</div>`);
}
function todayItems(title, items) {
  return panel(title, list(items).length ? `<div class="today-items">${items.map(inboxCard).join("")}</div>` : '<div class="panel-body muted">Сейчас здесь пусто.</div>');
}
async function today() {
  const d = await api("/today");
  const s = d.summary || {};
  const changes = list(d.new_changes);
  const changeBody = changes.length ? `<ul class="list">${changes.map(c => `<li><strong>${esc(c.kind)}</strong> · ${esc(c.description)}${c.company_name ? ` · ${esc(c.company_name)}` : ""} <span class="muted">${date(c.at)}</span></li>`).join("")}</ul>` : '<div class="panel-body muted">После следующего refresh здесь появятся изменения.</div>';
  return heading("Today", "Короткая очередь действий на сегодня.", '<a class="primary-link" href="/sync">Refresh Inbox →</a>') +
    `<div class="callout today-summary"><strong>${esc(s.text || "Сегодня: нет активных действий.")}</strong><p class="muted">Последний refresh: ${date(d.last_refresh_at)}</p></div>` +
    `<div class="workflow-overview"><a class="workflow-kpi" href="#needs-reply"><strong>Нужно ответить</strong><b>${s.needs_reply || 0}</b></a><a class="workflow-kpi" href="#needs-action"><strong>Нужно действие</strong><b>${s.needs_action || 0}</b></a><a class="workflow-kpi" href="#interviews"><strong>Интервью / тесты</strong><b>${s.interviews || 0}</b></a><a class="workflow-kpi" href="#follow-ups"><strong>Follow-up сегодня</strong><b>${s.follow_ups || 0}</b></a><a class="workflow-kpi" href="#changes"><strong>Новые изменения</strong><b>${changes.length}</b></a></div>` +
    `<div id="needs-reply">${todayItems("Нужно ответить", d.needs_reply)}</div><div id="needs-action">${todayItems("Нужно действие", d.needs_action)}</div><div id="interviews">${todayItems("Интервью / внешние тесты", d.interviews)}</div><div id="follow-ups">${todayItems("Follow-up сегодня", d.follow_ups)}</div><div id="changes">${panel("Новые важные изменения", changeBody)}</div>` +
    notificationPanel({ notifications: d.notifications });
}
function workflowOverview(m) {
  const values = [
    ["Нужно ответить", m.workflow_needs_reply || 0, "needs_reply"],
    ["Интервью / тесты", m.workflow_interviews || 0, "interviews"],
    ["Нужно действие", (m.workflow_needs_action || 0) + (m.workflow_clarifications || 0), "needs_action"],
    ["Ждём ответа", m.workflow_waiting || 0, "waiting"],
    ["Follow-up", m.follow_up_eligible || 0, "followups"],
  ];
  return panel("Рабочая очередь", `<div class="workflow-overview">${values.map(([label, value, id]) => `<a href="/inbox#${id}" class="workflow-kpi"><strong>${esc(label)}</strong><b>${value}</b></a>`).join("")}</div>`);
}
async function overview(dashboardPromise) {
  const d = await dashboardPromise;
  const [inbox, notifications] = await Promise.all([
    api("/inbox/overview"),
    api("/notifications/overview"),
  ]);
  const m = d.metrics;
  document.getElementById("inbox-count").textContent = m.workflow_important || "";
  return (
    heading(
      "Ваш следующий шаг начинается здесь",
      "Вакансии, диалоги и решения — в одном спокойном рабочем пространстве.",
      '<a class="secondary-link" href="/sync">⟳ Синхронизация</a><a class="primary-link" href="/inbox">Открыть Inbox →</a>',
    ) +
    metricsCards(m) + workflowOverview(m) + notificationPanel(notifications) +
    `<div class="grid-two"><div>${panel("В фокусе · Inbox", inbox.items.length ? inbox.items.slice(0, 5).map(inboxCard).join("") : empty("Входящие под контролем", "Новые диалоги появятся здесь после синхронизации."), '<a href="/inbox">Все диалоги →</a>')}</div><div>${panel("Поиск в цифрах", `<div class="panel-body"><div class="mini-stat"><span>Response rate</span><b>${m.response_rate.toFixed(1)}%</b></div><div class="mini-stat"><span>Interview conversion</span><b>${m.interview_conversion.toFixed(1)}%</b></div><div class="mini-stat"><span>Сообщения без ответа</span><b>${m.new_messages}</b></div></div>`, '<a href="/analytics">Analytics →</a>')}${panel("AI Assistant", `<div class="panel-body"><div class="mini-stat"><a href="/inbox">Drafts awaiting review</a><b>${m.ai_drafts}</b></div><div class="mini-stat"><a href="/knowledge/questions">Pending clarifications</a><b>${m.pending_clarifications}</b></div><div class="mini-stat"><a href="/knowledge/questions">Knowledge questions</a><b>${m.knowledge_questions}</b></div><div class="mini-stat"><a href="/knowledge/questions">Knowledge proposals</a><b>${m.pending_proposals}</b></div></div>`)}</div></div><div class="callout"><strong>Ваши решения остаются за вами</strong>AI готовит черновики и уточнения. Dashboard ничего не отправляет работодателям. Последняя успешная синхронизация: ${date(d.sync.last_success)}.</div>`
  );
}
function select(name, label, options, current = "") {
  return `<label>${label}<select name="${name}">${options.map(([value, text]) => `<option value="${esc(value)}" ${value === current ? "selected" : ""}>${esc(text)}</option>`).join("")}</select></label>`;
}
function filters(kind) {
  const q = new URLSearchParams(location.search);
  const common = `<label class="grow">Поиск<input name="search" type="search" placeholder="Вакансия или компания" value="${esc(q.get("search") || "")}"></label>${select(
    "recommendation",
    "Recommendation",
    [
      ["", "Все"],
      ["apply", "Apply"],
      ["maybe", "Maybe"],
      ["skip", "Skip"],
    ],
    q.get("recommendation") || "",
  )}`;
  const extra =
    kind === "applications"
      ? `${select("status", "Статус", [["", "Все статусы"], ...["discovered", "analyzed", "shortlisted", "applied", "employer_replied", "candidate_action_required", "waiting_employer", "interview", "offer", "rejected", "archived", "unknown"].map((v) => [v, statuses[v][0]])], q.get("status") || "")}<label>Компания<input name="company" value="${esc(q.get("company") || "")}" placeholder="Название"></label>${select(
          "sort",
          "Сортировка",
          [
            ["newest", "Сначала новые"],
            ["match_score", "Match score"],
            ["last_activity", "Последняя активность"],
          ],
          q.get("sort") || "newest",
        )}${select(
          "waiting_employer",
          "Ждём работодателя",
          [
            ["", "Все"],
            ["true", "Да"],
          ],
          q.get("waiting_employer") || "",
        )}${select(
          "need_candidate_response",
          "Нужен мой ответ",
          [
            ["", "Все"],
            ["true", "Да"],
          ],
          q.get("need_candidate_response") || "",
        )}`
      : `<label>Score от<input type="number" name="min_score" min="0" max="100" placeholder="0" value="${esc(q.get("min_score") || "")}"></label><label>до<input type="number" name="max_score" min="0" max="100" placeholder="100" value="${esc(q.get("max_score") || "")}"></label>`;
  return `<form class="filter-bar" data-filter>${common}${extra}<button type="submit">Применить</button><a href="/${kind}">Сбросить</a></form>`;
}
function attentionCard(item) {
  return `<article class="question"><p>${badge(item.type)} <strong>${esc(item.title || "Требуется внимание")}</strong></p><p>${esc(item.summary || item.reason || "")}</p><p class="muted">${esc(item.risk || "manual review")} · ${date(item.updated_at)}</p><p>${esc(item.next_action || "Проверить вручную")}</p></article>`;
}
async function careerAgentWorkspace() {
  const [attention, metrics, runs] = await Promise.all([api("/career/attention"), api("/career/metrics"), api("/career/runs")]);
  const items = list(attention.items);
  const runList = list(runs.runs).slice(0, 8).map((run) => `<li>${badge(run.status)} <strong>${esc(run.run_type || run.id)}</strong> · ${date(run.started_at)}</li>`).join("");
  return heading("Career Agent Control Center", "Единая read-only очередь для вакансий и коммуникаций.", '<button class="primary" data-action="career-daily-run">Run daily</button>') +
    panel("Daily status", `${pairs([["Last run", runList ? "см. timeline" : "не запускался"], ["Review queue", items.length], ["Matched vacancies", metrics.matched || 0], ["Run status", metrics.run_status || "—"]])}<p class="muted">Run daily выполняет только HH reads, AI analysis и локальную telemetry. Approval/send flow остаётся отдельным.</p>`) +
    panel("Unified Attention Queue", items.length ? items.map(attentionCard).join("") : empty("Очередь пуста", "Новых безопасных действий для ручной проверки нет.", "")) +
    panel("Run timeline", runList ? `<ul class="list">${runList}</ul>` : '<p class="muted">Durable runs пока не записаны.</p>');
}
async function applications() {
  const rows = await api(`/applications${location.search}`);
  return (
    heading("Applications", "От первого отклика до следующего предложения.") +
    filters("applications") +
    panel(`${rows.length} записей`, applicationTable(rows))
  );
}
function queueReasons(reasons) {
  return list(reasons).map((reason) => `<li>${esc(reason.evidence || reason.code)}</li>`).join("") || '<li class="muted">Нет сохранённых пояснений</li>';
}
function queueCard(item) {
  const title = item.title || `Вакансия #${item.vacancy_id}`;
  const reviewActions = item.application_state === "none" && item.rankable ? `<button data-action="vacancy-review" data-review="seen" data-id="${esc(item.vacancy_id)}">Отметить просмотренной</button><button data-action="vacancy-review" data-review="interesting" data-id="${esc(item.vacancy_id)}">Интересна</button><button data-action="vacancy-review" data-review="dismiss" data-id="${esc(item.vacancy_id)}">Скрыть</button>` : "";
  return `<article class="question vacancy-queue-card"><div class="queue-card-heading"><div><h3>${link(item.detail_path, title)}</h3><p class="muted">${esc(item.company || "Компания не указана")}</p></div><div class="queue-badges">${badge(item.section)} ${badge(item.eligibility)} ${badge(item.fit_band)}</div></div><div class="queue-meta">${badge(item.confidence)} ${badge(item.analysis_state)} <span class="score">${esc(item.base_rank_score)}<span class="muted"> / 100 · базовый ранг</span></span></div><p class="muted">${esc([item.salary || "Зарплата неизвестна", item.location || "Локация неизвестна", item.work_format || "Формат неизвестен", item.freshness_label].join(" · "))}</p>${item.changed_since_review ? '<p class="callout compact">Обновлено после вашего решения</p>' : ""}<div class="queue-reasons"><div><strong>Почему посмотреть</strong><ul>${queueReasons(item.positive_reasons)}</ul></div><div><strong>Ограничения / неизвестно</strong><ul>${queueReasons([...list(item.concerns), ...list(item.unknowns)])}</ul>${item.unknown_count ? `<span class="muted">Ещё неизвестных полей: ${esc(item.unknown_count)}</span>` : ""}</div></div><div class="actions"><a class="secondary-link" href="${esc(item.detail_path)}">Открыть и проверить</a>${reviewActions}</div></article>`;
}
function rankedQueueFilters(q) {
  return `<form class="filter-bar" data-filter><label>Поиск<input name="search" value="${esc(q.get("search") || "")}" placeholder="Название, компания, локация"></label>${select("section", "Раздел", [["", "Все активные"], ["to_review", "К просмотру"], ["worth_another_look", "Вернуться позже"], ["stretch_manual_review", "Stretch / ручная проверка"], ["closed_excluded", "Закрытые / исключённые"]], q.get("section") || "")} ${select("eligibility", "Состояние пригодности", [["", "Все"], ["eligible", "Известно подходит"], ["review_required", "Нужна проверка"], ["ineligible", "Не подходит"], ["unavailable", "Недоступна"]], q.get("eligibility") || "")}<button type="submit">Применить</button><a href="/vacancies">Сбросить</a></form>`;
}
async function vacancies() {
  const q = new URLSearchParams(location.search);
  const legacyPage = (rows) => heading("Vacancies", "Полный legacy-список для диагностики и доступа к исходным фильтрам.", link("/vacancies", "← Ranked queue")) + filters("vacancies") + panel(`${rows.length} вакансий`, rows.length ? `<div class="table-wrap"><table><thead><tr><th>Вакансия / компания</th><th>Зарплата</th><th>Match</th><th>Recommendation</th><th>Опубликовано</th></tr></thead><tbody>${rows.map((v) => `<tr><td>${link(`/vacancies/${v.id}`, v.title || v.name)}<span class="muted">${esc(v.company?.name)}</span></td><td>${esc([v.salary, v.salary_currency].filter(Boolean).join(" ") || "Не указана")}</td><td>${score(v.match_result)}</td><td>${badge(v.application_recommendation?.decision || v.match_result?.recommendation?.decision)}</td><td>${date(v.published_at)}</td></tr>`).join("")}</tbody></table></div>` : empty("Вакансии не найдены", "Сохранённые реальные вакансии появятся после HH sync."));
  if (q.get("view") === "all") {
    const rows = await api(`/vacancies${location.search}`);
    return legacyPage(rows);
  }
  let data;
  try { data = await api(`/vacancies/ranked${location.search}`); } catch (error) { if (error.status !== 503) throw error; const rows = await api(`/vacancies${location.search}`); return legacyPage(rows); }
  const c = data.counts || {};
  const tabs = `<div class="tabs"><a class="active" href="/vacancies">Ranked queue</a><a href="/vacancies?view=all">All vacancies</a></div>`;
  const summary = panel("Снимок очереди", `<div class="panel-body"><div class="queue-summary">${pairs([["К просмотру", c.to_review || 0], ["Вернуться позже", c.worth_another_look || 0], ["Stretch / ручная проверка", c.stretch_manual_review || 0], ["Закрытые / исключённые", c.closed_excluded || 0], ["Нужна проверка", c.review_required || 0], ["Известно подходит", c.eligible || 0]])}</div><p class="muted">Рейтинг помогает выбрать следующий просмотр. Он не разрешает отклик и не заменяет свежую preflight-проверку.</p></div>`);
  return heading("Vacancies", "Сначала — вакансии, которые стоит проверить сегодня. Неизвестные данные остаются видимыми.", link("/sync", "Обновить данные")) + tabs + summary + rankedQueueFilters(q) + panel(`${data.total || 0} в текущем фильтре · страница ${Math.floor((data.offset || 0) / (data.limit || 25)) + 1}`, list(data.items).length ? list(data.items).map(queueCard).join("") + (data.has_more ? `<div class="actions"><a class="secondary-link" href="/vacancies?offset=${esc(data.next_offset)}${q.get("section") ? `&section=${encodeURIComponent(q.get("section"))}` : ""}${q.get("search") ? `&search=${encodeURIComponent(q.get("search"))}` : ""}">Показать ещё</a></div>` : "") : empty("В этом разделе пока пусто", "Попробуйте другой раздел или выполните read-only HH sync."));
}
function matchPanel(m) {
  return panel(
    "Match analysis",
    m
      ? `<div class="panel-body"><div class="actions">${score(m)} ${badge(m.recommendation?.decision)}</div><p>${esc(m.explanation)}</p><h3>Подтверждённые совпадения</h3>${ul(m.matched_skills)}<h3>Недостающие навыки</h3>${ul(m.missing_skills)}<h3>Неизвестные навыки</h3>${ul(m.unknown_skills)}<h3>Риски</h3>${ul(m.risks)}<p class="muted">${esc(m.recommendation?.reason)}</p></div>`
      : empty(
          "Вакансия ещё не оценена",
          "Оценка появится после анализа через существующий pipeline.",
          "",
        ),
  );
}
function vacancyPanel(v) {
  return panel(
    "Вакансия",
    v
      ? `<div class="panel-body">${pairs([
          ["Компания", v.company?.name],
          ["Зарплата", [v.salary, v.salary_currency].filter(Boolean).join(" ")],
          ["Локация", v.location || v.area?.name],
          ["Формат работы", v.work_format],
          ["Тип занятости", v.employment_type],
          ["Опубликовано", date(v.published_at)],
        ])}<h3>Требования</h3>${ul(v.requirements)}<details><summary>Полное описание</summary><p class="text-block">${esc(v.description || "Описание отсутствует")}</p></details></div>`
      : empty(
          "Вакансия ещё не синхронизирована",
          "Откройте Sync → Vacancies для загрузки подробностей.",
        ),
  );
}
function rankingDetailPanel(ranking, events) {
  if (!ranking) return empty("Ranking недоступен", "Очередь требует canonical PostgreSQL candidate/review read model.", "");
  const reasons = (title, values) => `<h3>${esc(title)}</h3>${list(values).length ? `<ul class="list">${values.map((r) => `<li>${esc(r.evidence || r.code)} <span class="muted">· ${esc(r.source || "")}</span></li>`).join("")}</ul>` : '<p class="muted">Нет</p>'}`;
  return panel("Детерминированный результат очереди", `<div class="panel-body"><div class="queue-badges">${badge(ranking.eligibility)} ${badge(ranking.fit_band)} ${badge(ranking.confidence)} ${badge(ranking.analysis_state)}</div>${pairs([["Баллы очереди", `${ranking.base_rank_score} / 100`], ["Раздел", ranking.section], ["Состояние просмотра", ranking.review_state], ["Состояние отклика", ranking.application_state], ["Свежесть данных", ranking.freshness_label], ["Изменилось после решения", ranking.changed_since_review == null ? "Неизвестно" : ranking.changed_since_review ? "Да" : "Нет"]])}${reasons("Положительные сигналы", ranking.positive_reasons_all)}${reasons("Ограничения", ranking.concerns_all)}${reasons("Неизвестно", ranking.unknowns_all)}${reasons("Жёсткие причины", ranking.hard_reasons_all)}${events?.length ? `<h3>История решений</h3>${ul(events.map((e) => `${e.type} · ${date(e.occurred_at)}${e.reason ? ` · ${e.reason}` : ""}`))}` : ""}<div class="actions">${ranking.application_state === "none" && ranking.rankable ? `<button data-action="vacancy-review" data-review="seen" data-id="${esc(ranking.vacancy_id)}">Отметить просмотренной</button><button data-action="vacancy-review" data-review="interesting" data-id="${esc(ranking.vacancy_id)}">Интересна</button><button data-action="vacancy-review" data-review="dismiss" data-id="${esc(ranking.vacancy_id)}">Скрыть</button>` : ""}<p class="muted">Подготовка отклика остаётся отдельным существующим application flow: queue не отправляет отклики.</p></div></div>`);
}
async function vacancyDetail(id) {
  const v = await api(`/vacancies/${urlID(id)}`);
  let detail = null;
  try { detail = await api(`/vacancies/${urlID(id)}/ranking`); } catch (error) { if (error.status !== 503) throw error; }
  return (
    heading(
      v.title || v.name,
      v.company?.name || "Вакансия",
      safeHHLink(v.links?.desktop || v.links?.alternate),
    ) +
    `<div class="detail-grid"><div>${vacancyPanel(v)}</div><div>${rankingDetailPanel(detail?.ranking, detail?.review_events)}${matchPanel(v.match_result)}</div></div>`
  );
}
function knowledgeContext(ctx, used = []) {
  return panel(
    "Knowledge context",
    `<div class="panel-body"><h3>Факты, использованные в draft</h3>${ul(used)}<h3>Доступные подтверждённые факты</h3>${ul(ctx?.allowed_facts)}<h3>Forbidden claims</h3>${ul(ctx?.forbidden_claims)}<h3>Недостающая информация</h3>${ul(ctx?.missing_information)}</div>`,
  );
}
function messages(c) {
  return panel(
    "Переписка",
    list(c?.messages).length
      ? `<div class="messages">${c.messages.map((m) => `<div class="message ${m.hh_system_event || m.sender === "system" ? "system" : m.direction === "outgoing" ? "outgoing" : "incoming"}"><div class="bubble">${esc(m.text || (m.content_unavailable && !m.hh_system_event ? "Содержимое сообщения недоступно" : "Служебное событие"))}</div><div class="message-meta">${esc(m.hh_system_event ? "Система" : m.sender === "employer" ? "Работодатель" : m.sender === "candidate" ? "Вы" : m.sender === "system" ? "Система" : "Неизвестный отправитель")} · ${date(m.timestamp)}${m.source === "ai_draft" ? " · локальный draft" : ""}</div></div>`).join("")}</div>`
      : empty(
          "История сообщений пуста",
          "Синхронизируйте диалоги, чтобы увидеть переписку.",
        ),
  );
}
function assistantPanel(ai, conversationID, context, conversation = null, warning = "") {
  const d = ai?.latest_draft;
  // The backend separates lifecycle selection from audit history. Do not
  // infer the current action from timestamp order: a historical failed send
  // may have a later updated_at than a fresh approved action.
  const action = ai?.current_action || null;
  const actionHistory = list(ai?.previous_attempts);
  const actionState = action?.lifecycle_status || action?.status;
  const approved = actionState === "approved" ? action : null;
  const unresolvedAction = action && ["sent_unconfirmed", "delivery_uncertain", "manual_review"].includes(actionState) ? action : null;
  const decision = ai?.decision?.action ? ai.decision : null;
  if (d) draftsForCopy.set(d.id, d.text);
  const lastEmployer = list(conversation?.messages).filter((m) => m.sender === "employer").at(-1);
  const contextWarnings = list(context?.consistency_warnings);
  const unresolved = list(context?.unresolved_questions);
  const warningList = [...contextWarnings.map((w) => w.message || w.code || "consistency warning"), ...unresolved.map((q) => q.question || "candidate clarification")];
  const candidateFacts = list(d?.used_facts).length ? d.used_facts : list(context?.candidate_context?.allowed_facts);
  const quality = ai?.draft_quality;
  const draftFeedback = d ? `<div class="quality-actions"><span class="muted">Draft quality:</span><button data-action="draft-feedback-good" data-id="${esc(d.id)}">Хороший ответ</button><select data-draft-reason="${esc(d.id)}" aria-label="Причина исправления"><option value="слишком формально">слишком формально</option><option value="слишком длинно">слишком длинно</option><option value="звучит как AI">звучит как AI</option><option value="неверный факт">неверный факт</option><option value="пропущен факт">пропущен факт</option><option value="не понял вопрос">не понял вопрос</option><option value="плохая формулировка">плохая формулировка</option><option value="другое">другое</option></select><button data-action="draft-feedback-edit" data-id="${esc(d.id)}">Нужно исправить</button></div>` : "";
  const sendReady = approved && action.safety === "READY_TO_SEND" && action.request_validation === "VALID";
  const sendLabel = action?.write_capability === "ENABLED" ? "Send to HH" : "Send to HH (blocked safely)";
  const history = actionHistory.length
    ? `<details class="action-history"><summary>Previous attempts / History (${actionHistory.length})</summary><div class="table-wrap"><table><thead><tr><th>Action</th><th>Created</th><th>Status</th><th>Content hash</th><th>Nonce</th><th>Result</th></tr></thead><tbody>${actionHistory.map((item) => `<tr><td>${esc(item.id)}</td><td>${date(item.created_at)}</td><td>${badge(item.lifecycle_status || item.status)}</td><td class="text-block">${esc(item.content_hash || "—")}</td><td>${item.nonce_used_at ? `used · ${date(item.nonce_used_at)}` : item.send_nonce ? `fresh · ${esc(String(item.send_nonce).length)} chars` : "—"}</td><td>${esc(item.error || item.transport_response?.result || "—")}</td></tr>`).join("")}</tbody></table></div></details>`
    : "";
  const approvalCard = approved
    ? `<div class="callout"><strong>Manual confirmation required</strong>${pairs([["Company", conversation?.company_name || "—"], ["Vacancy", conversation?.vacancy_title || "—"], ["Destination chat.id", conversation?.hh_conversation_id || "—"], ["Last employer message", lastEmployer ? date(lastEmployer.timestamp) : "—"], ["Draft source", d?.source || "unknown"], ["Last preflight", date(approved.last_preflight_at)], ["Safety", action.safety || "BLOCKED"], ["Request validation", action.request_validation || "INVALID"], ["Write capability", action.write_capability || "DISABLED"]])}<p><strong>Последнее сообщение работодателя</strong></p><div class="draft">${esc(lastEmployer?.text || "Содержимое недоступно")}</div><p><strong>Точный approved текст</strong></p><div class="draft">${esc(approved.approved_text)}</div><p><strong>Candidate facts used</strong></p>${ul(candidateFacts)}<p><strong>Warnings</strong></p>${warningList.length ? ul(warningList) : '<p class="muted">none</p>'}${sendReady ? (action.write_capability === "ENABLED" ? '<p class="success">LIVE TRANSPORT READY · Waiting for explicit user Send</p>' : `<p class="muted">SAFE DIAGNOSTIC · ${esc(action.write_capability || "WRITE_DISABLED")} · HH writes disabled; HHWriteClient will not be called</p>`) : '<p class="muted">Сначала выполните fresh sync + preflight. Send останется заблокированным до READY_TO_SEND.</p>'}<div class="actions">${sendReady ? `<button type="button" class="primary" data-action="send-action" data-id="${esc(approved.id)}" data-nonce="${esc(approved.send_nonce || "")}">${sendLabel}</button>` : `<button type="button" class="primary" data-action="preflight-action" data-id="${esc(approved.id)}">Run fresh preflight</button><button type="button" data-action="cancel-action" data-id="${esc(approved.id)}">Cancel</button>`}</div></div>`
    : actionState === "stale"
      ? `<div class="callout"><strong>Approval stale</strong><p>Причина: ${esc(action.error || list(action.lifecycle_reasons).join("; ") || list(action.last_preflight_reasons).join("; ") || "Candidate knowledge relevant to this draft changed")}</p>${action.current_relevant_knowledge_hash ? pairs([["Approved knowledge hash", String(action.relevant_knowledge_hash).slice(0, 10) + "…"], ["Current knowledge hash", String(action.current_relevant_knowledge_hash).slice(0, 10) + "…"]]) : ""}${list(action.relevant_knowledge_diff).length ? `<p><strong>Semantic diff</strong></p>${ul(action.relevant_knowledge_diff)}` : ""}<p class="muted">Точный approved текст сохранён и не был отправлен.</p><div class="actions"><button data-action="review-action" data-id="${esc(action.id)}">Review changes</button><button class="primary" data-action="approve-draft" data-id="${esc(action.draft_id)}">Approve again</button><button data-action="cancel-action" data-id="${esc(action.id)}">Cancel</button></div></div>`
      : actionState === "cancelled"
        ? `<div class="callout"><strong>Cancelled</strong><p class="muted">Это действие отменено и больше не отправляется.</p></div>`
        : actionState === "sent" || actionState === "delivery_confirmed"
          ? `<div class="callout"><strong>Delivery status</strong><p class="success">${badge(actionState)} ${esc(action.external_message_id || "external message id unavailable")}</p></div>`
	          : actionState === "failed"
	            ? `<div class="callout"><strong>Failure status</strong><p class="error">${esc(action.error || "HH write failed")}</p>${action.transport_response ? `<p class="error">HTTP ${esc(action.transport_response.http_status || "—")} · ${esc(action.transport_response.result || "bad_request")}</p><p><strong>HH response</strong></p><div class="draft">${esc(action.transport_response.response_body || "Response body unavailable")}</div>` : ""}<p class="muted">Retry запрещён автоматически. External message: ${esc(action.external_message_id || "отсутствует")}</p></div>`
            : unresolvedAction
      ? `<div class="callout"><strong>Delivery requires reconciliation</strong><p>${badge(unresolvedAction.status)} ${esc(unresolvedAction.error || "Повторная отправка запрещена")}</p><p class="muted">External message: ${esc(unresolvedAction.external_message_id || "неизвестен")}</p><button data-action="reconcile-action" data-id="${esc(unresolvedAction.id)}">Run read-only reconciliation</button></div>`
      : "";
  return panel(
    "AI Assistant",
    `<div class="panel-body"><div class="eyebrow">ПОДГОТОВКА ОТВЕТА</div>${warning ? `<p class="error">${esc(warning)}</p>` : ""}${ai?.action_selection_reason ? `<p class="muted">Current action: ${esc(ai.action_selection_reason)}</p>` : ""}${decision ? `<div class="callout">${badge(decision.action)}<p>${esc(decision.reason)}</p>${ul(decision.warnings)}${decision.missing_information?.length ? ul(decision.missing_information) : ""}</div>` : ""}${approvalCard}${d ? `<p>${badge(d.status)} <span class="muted">${date(d.created_at)}</span></p><textarea class="draft-editor" data-draft-editor="${esc(d.id)}">${esc(d.text)}</textarea><p class="muted">${esc(d.decision_reason)}</p>` : '<p class="muted">Черновика пока нет. Подготовьте ответ на основе переписки и подтверждённых фактов.</p>'}<div class="actions">${conversationID ? `<button class="primary" data-action="draft" data-id="${esc(conversationID)}">${d ? "Regenerate draft" : "Generate AI Draft"}</button>` : ""}${d ? `<button data-action="edit-draft" data-id="${esc(d.id)}">Save edited draft</button><button data-action="copy" data-id="${esc(d.id)}">Copy draft</button>${d.status === "generated" ? `<button class="primary" data-action="approve-draft" data-id="${esc(d.id)}">${actionState === "stale" ? "Approve again" : "Approve exact text"}</button><button data-action="reject" data-id="${esc(d.id)}">Reject draft</button>` : ""}` : ""}</div>${draftFeedback}${quality ? `<h3>Draft quality check</h3>${pairs([["Facts used", list(quality.facts_used).join(", ") || "—"], ["Projects used", list(quality.projects_used).join(", ") || "—"], ["Conversation context used", quality.conversation_context_used ? "yes" : "no"], ["Forbidden claims checked", quality.forbidden_claims_checked ? "yes" : "no"], ["Source", quality.source || "—"]])}${list(quality.warnings).length ? `<h4>Warnings</h4>${ul(quality.warnings)}` : ""}` : ""}<p class="muted">Approve, Preflight и Send — три отдельных действия. HH writes: manual approval only.</p>${history}${list(ai?.clarifications).length ? `<h3>Candidate clarifications</h3>${ai.clarifications.map((c) => `<p>${esc(c.question)} ${badge(c.status)}</p>`).join("")}<a href="/knowledge/questions">Ответить на вопросы →</a>` : ""}${list(context?.consistency_warnings).length ? `<h3>Consistency warnings</h3>${ul(context.consistency_warnings)}` : ""}</div>`,
  );
}
async function applicationDetail(id) {
  const d = await api(`/applications/${urlID(id)}`);
  const a = d.application,
    c = d.conversation_detail;
  return (
    heading(a.vacancy_title, a.company_name, safeHHLink(a.vacancy_url)) +
    `<div class="detail-grid"><div>${panel(
      "Application",
      `<div class="panel-body">${badge(a.display_status)}${pairs([
        ["Дата отклика", date(a.applied_at)],
        ["Следующее действие", statuses[a.next_action]?.[0] || a.next_action],
        ["Последний контакт", date(a.last_contact)],
      ])}<h3>Timeline</h3><ul class="timeline">${list(d.timeline)
        .map(
          (e) =>
            `<li>${badge(e.type)}<p>${esc(e.description)}</p><time>${date(e.timestamp)}</time></li>`,
        )
        .join("")}</ul></div>`,
    )}${vacancyPanel(d.vacancy)}${c ? messages(c.conversation) : panel("Переписка", empty("Диалог ещё не привязан", "Синхронизируйте отклики и переписки."))}</div><div>${followUpPanel(d.follow_up, d.follow_up_draft)}${matchPanel(d.match_result)}${assistantPanel(d.ai, a.conversation_id, c?.context, c?.conversation, c?.context_warning)}${a.conversation_id ? `<p>${link(`/conversations/${urlID(a.conversation_id)}`, "Открыть диалог →")}</p>` : ""}${knowledgeContext(d.candidate_context, d.ai?.latest_draft?.used_facts)}</div></div>`
  );
}
function followUpPanel(f, draft) {
  if (!f) return "";
  return panel("Follow-up", `<div class="panel-body">${badge(f.status)}${pairs([
    ["Waiting since", date(f.waiting_since)], ["Ждём, дней", Number(f.days_waiting || 0).toFixed(1)],
    ["Reason", f.reason], ["Previous follow-ups", f.previous_followups], ["Next eligible date", date(f.eligible_at)]
  ])}${ul(f.warnings)}${draft ? `<h3>AI draft</h3><div class="draft">${esc(draft.text)}</div><p class="muted">Черновик: ${date(draft.created_at)}. Проверьте актуальную eligibility перед использованием.</p>` : ""}<div class="actions">${f.status === "eligible" ? `<button class="primary" data-action="follow-up-draft" data-id="${esc(f.application_id)}">Generate draft</button>` : ""}<button data-action="follow-up-dismiss" data-id="${esc(f.application_id)}">Don't follow up</button></div></div>`);
}
function followUpCard(f) {
  return `<article class="question"><h3>${link(`/applications/${urlID(f.application_id)}`, f.company_name || "Компания")}</h3><p>${esc(f.vacancy_title)}</p><p>Ждём ${Number(f.days_waiting).toFixed(1)} дней</p><p><strong>AI recommendation:</strong> Можно подготовить follow-up</p><div class="actions"><button class="primary" data-action="follow-up-draft" data-id="${esc(f.application_id)}">Generate draft</button><button data-action="follow-up-dismiss" data-id="${esc(f.application_id)}">Don't follow up</button></div></article>`;
}
function workflowHero(workflow, conversation, ai) {
  if (!workflow) return "";
  const draft = ai?.latest_draft;
  const external = workflow.external_action;
  const waiting = workflow.waiting;
  return panel(
    workflow.label || "Состояние диалога",
    `<div class="workflow-hero"><div class="hero-state">${badge(workflow.state)}<h2>${esc(workflow.what_is_happening || "Состояние определено локально")}</h2><p>${esc(workflow.what_to_do || "Откройте Diagnostics для подробностей.")}</p></div>${external ? `<div class="external-card"><h3>Внешнее действие</h3>${pairs([["Компания", conversation.company_name], ["Вакансия", conversation.vacancy_title], ["Что требуется", external.required_action], ["Ссылка", external.destination || "—"], ["Длительность", external.duration], ["Deadline / aging", external.aging], ["Статус", workflow.label]])}${external.result_timing ? `<p class="muted">${esc(external.result_timing)}</p>` : ""}${safeExternalLink(external.destination)}</div>` : ""}${waiting ? `<div class="waiting-card">${pairs([["Наш последний ответ", date(waiting.last_candidate_reply_at)], ["Ждём", waiting.waiting_since ? `${Math.max(0, Math.round(waiting.elapsed_seconds / 3600))} ч.` : "—"], ["Follow-up", workflow.follow_up?.eligible ? `Suggestion доступен · ${date(workflow.follow_up.recommended_follow_up_date)}` : "Пока рано"]])}</div>` : ""}${workflow.ai_proposal ? `<div class="ai-suggestion"><strong>AI:</strong><p>${esc(workflow.ai_proposal)}</p>${draft ? `<div class="draft">${esc(draft.text)}</div><div class="actions"><button data-action="copy" data-id="${esc(draft.id)}">Copy</button><button class="primary" data-action="approve-draft" data-id="${esc(draft.id)}">Approve</button></div>` : ""}</div>` : ""}</div><details class="diagnostics"><summary>Diagnostics</summary>${pairs([["Technical state", workflow.state], ["Priority", workflow.priority], ["Last employer message hash", workflow.last_employer_message_hash || "—"], ["Follow-up date", date(workflow.follow_up?.recommended_follow_up_date)]])}${ul(workflow.diagnostics)}</details>`,
  );
}
async function healthPage(deep = false) {
  const d = await api(deep ? "/health/deep" : "/health");
  if (!deep) {
    return heading("System Health", "Быстрый локальный снимок состояния приложения.", '<button data-action="deep-health">Run deep audit</button>') +
      panel("Fast health", `<div class="panel-body">${pairs([["Mode", d.mode], ["Process", d.process], ["HH write capability", d.write_gateway?.capability], ["HH dry-run", d.write_gateway?.dry_run], ["Last successful sync", date(d.sync?.last_success)], ["Last sync error", d.sync?.last_error || "—"]])}${pairs(Object.entries(d.counts || {}))}<p class="muted">Fast health does not run eligibility or reconciliation diagnostics.</p></div>`) +
      panel("Sync status", `<div class="panel-body"><div id="health-sync-status">${syncProgress(d.sync_state || {})}</div></div>`);
  }
  const eligibility = d.manual_reply_eligibility || {};
  const eligibilitySummary = eligibility.summary || {};
  const lifecycle = list(d.write_gateway?.send_lifecycle);
  const lifecycleMarkup = lifecycle.length
    ? `<div class="table-wrap"><table><thead><tr><th>Time</th><th>Action</th><th>Event</th><th>Reason</th></tr></thead><tbody>${lifecycle
        .slice(-40)
        .reverse()
        .map((e) => `<tr><td>${date(e.at)}</td><td>${esc(e.action_id)}</td><td>${esc(e.type)}</td><td>${esc(e.reason || "—")}</td></tr>`)
        .join("")}</tbody></table></div>`
    : '<p class="muted">Send lifecycle пока пуст.</p>';
  const eligibilityRows = list(eligibility.conversations).filter((r) => r.classification !== "SAFE_FOR_MANUAL_REPLY").slice(0, 80);
  const eligibilityTable = eligibilityRows.length
    ? `<div class="table-wrap"><table><thead><tr><th>Conversation</th><th>Class</th><th>Destination</th><th>State</th><th>Blockers</th></tr></thead><tbody>${eligibilityRows.map((r) => `<tr><td>${link(`/conversations/${urlID(r.conversation_id)}`, r.conversation_id)}</td><td>${badge(String(r.classification || "").toLowerCase())}</td><td>${esc(r.destination_status || "—")}</td><td>${esc(r.state_status || "—")}</td><td>${esc(list(r.blockers).map((b) => b.code).join(", ") || "—")}</td></tr>`).join("")}</tbody></table></div>`
    : `<p class="muted">Нет заблокированных или требующих ручной проверки диалогов.</p>`;
  const topBlockers = list(eligibilitySummary.top_blockers).map((b) => `${b.code} · ${b.count}`);
  const firstPilot = d.write_gateway?.first_pilot;
  const firstPilotPanel = firstPilot ? panel("First pilot", `<div class="panel-body">${pairs([["Action", firstPilot.action_id], ["Delivery", firstPilot.delivery], ["Attempts", firstPilot.attempts], ["Successful", firstPilot.successful], ["Failed", firstPilot.failed]])}<p class="muted">${esc(firstPilot.historical_failures_note)}</p></div>`) : "";
  const excludedApproved = list(d.write_gateway?.excluded_approved_actions);
  return heading("System Health / Data Quality", "Глубокая диагностика локального состояния. Предупреждения не исправляют данные автоматически.", '<button data-action="fast-health">Fast health</button>') +
    panel("HH Write Gateway", `<div class="panel-body">${pairs([["Capability", d.write_gateway?.capability || "DISABLED"], ["HH write enabled", d.write_gateway?.hh_write_enabled], ["Dry run", d.write_gateway?.dry_run], ["Last write", date(d.write_gateway?.last_write?.created_at)], ["Pending approved", d.write_gateway?.pending_approved_actions || 0], ["Uncertain deliveries", d.write_gateway?.uncertain_deliveries || 0], ["Failed writes", d.write_gateway?.failed_writes || 0], ["Write attempts", d.write_gateway?.metrics?.write_attempts_total ?? d.write_gateway?.metrics?.manual_writes_total ?? 0], ["Successful writes", d.write_gateway?.metrics?.successful_writes || 0], ["Delivery confirmed", d.write_gateway?.metrics?.delivery_confirmed || 0], ["Stale before send", d.write_gateway?.metrics?.stale_before_send || 0], ["Blocked by preflight", d.write_gateway?.metrics?.blocked_by_preflight || 0]])}${excludedApproved.length ? `<h3>Excluded approved actions</h3>${ul(excludedApproved.map((a) => `${a.action_id} · ${a.status} · ${a.sendability} · ${a.reason}`))}` : ""}<p class="muted">Каждая отправка требует отдельного Approve, fresh Preflight и Send. Dry-run всегда блокирует запись. Write attempts не означают, что сообщение доставлено.</p></div>`) +
    firstPilotPanel +
    panel("Dashboard Send lifecycle", `<div class="panel-body">${lifecycleMarkup}<p class="muted">UI/API lifecycle не являются HH write attempts. Только send_started считается попыткой transport.</p></div>`) +
    panel("Manual Reply Eligibility", `<div class="panel-body">${pairs([["SAFE", eligibilitySummary.safe_for_manual_reply || 0], ["BLOCKED", eligibilitySummary.blocked || 0], ["MANUAL REVIEW", eligibilitySummary.manual_review || 0]])}<h3>Top blockers</h3>${ul(topBlockers)}${eligibilityTable}<p><a href="/api/eligibility" target="_blank" rel="noopener noreferrer">Открыть безопасный JSON-отчёт без текста сообщений →</a></p></div>`) +
    panel("Stores", `<div class="panel-body">${pairs(Object.entries(d.stores))}${list(d.store_errors).length ? ul(d.store_errors) : `<p class="muted">JSON stores читаются без ошибок.</p>`}</div>`) +
    panel("Sync", `<div class="panel-body">${pairs([["Last success", date(d.sync.last_success)], ["Last error", d.sync.last_error || "—"], ["Unknown HH statuses", d.unknown_hh_statuses]])}<a href="/sync">Открыть Sync →</a></div>`) +
    panel("Relations", `<div class="panel-body">${pairs(Object.entries(d.relations))}</div>`) +
    panel(`Critical · ${(d.critical || []).length}`, `<div class="panel-body">${ul((d.critical || []).map(w => `${w.code} · ${w.record_type} ${w.record_id || ""}`))}</div>`) +
    panel(`Warnings · ${(d.actual_warnings || []).length}`, `<div class="panel-body">${pairs(Object.entries(d.actual_warning_counts || {}))}${ul((d.actual_warnings || []).map(w => `${w.code} · ${w.record_type} ${w.record_id || ""}`))}</div>`) +
    panel(`Incomplete data · ${(d.incomplete_data || []).length}`, `<div class="panel-body">${ul((d.incomplete_data || []).map(w => `${w.code} · ${w.record_type} ${w.record_id || ""}`))}</div>`) +
    panel(`Informational · ${(d.informational || []).length}`, `<div class="panel-body">${ul((d.informational || []).map(w => `${w.code} · ${w.record_type} ${w.record_id || ""}`))}</div>`);
}
function followUpMetrics(m) {
 if (!m) return "";
 const avg = (value, n) => value == null ? `Недостаточно данных (n=${n})` : `${Number(value).toFixed(1)} ч (n=${n})`;
 return panel("Response & follow-up", `<div class="panel-body">${pairs([
 ["Average employer response time", avg(m.average_employer_response_hours, m.employer_response_samples)],
 ["Average waiting time", avg(m.average_waiting_hours, m.waiting_samples)],
 ["Follow-up eligible", m.follow_up_eligible], ["Follow-up drafted", m.follow_up_drafted],
 ["Conversations needing candidate response", m.candidate_action_required]
 ])}<p class="muted">Средние показываются от ${m.minimum_samples} наблюдений. Неполная история исключается; эти значения не прогнозируют ответ работодателя.</p></div>`);
}
async function inbox() {
  const d = await api("/inbox");
  const grouped = workflowItemsBySection(list(d.items), d.sections);
  const bucketSummary = list(d.buckets).map((value) => `${esc(value.bucket)}: ${value.count || 0}`).join(" · ");
  return (
    heading(
      "Inbox",
      "Понятные следующие шаги по каждому диалогу — без необходимости разбираться в lifecycle-кодах.",
      '<button class="primary" data-action="sync" data-id="inbox">Refresh Inbox</button><a class="secondary-link" href="/knowledge/questions">Knowledge questions →</a>',
    ) +
    `<div class="important-count"><strong>${d.important_count || 0}</strong><span>важных элементов</span><span class="muted">из ${d.items?.length || 0} диалогов</span></div><p class="muted">Buckets: ${bucketSummary || "—"}</p>` +
    `<div class="workflow-sections">${list(d.sections).map((section) => workflowSectionCard(section, grouped[section.id] || [])).join("")}</div>` +
    panel("Follow-up suggestions", list(d.follow_ups).length ? d.follow_ups.map(followUpCard).join("") : `<div class="panel-body muted">Сейчас нет подходящих follow-up. Suggestions не отправляются автоматически.</div>`) +
    `<div class="callout">Приоритет: интервью / внешнее действие → нужно ответить → уточнение → follow-up → ожидание → без действий. Технические lifecycle-поля доступны в Diagnostics.</div>`
  );
}
async function conversationDetail(id) {
  const d = await api(`/conversations/${urlID(id)}`),
    c = d.conversation;
  return (
    heading(c.company_name, c.vacancy_title + " · local state first", `${d.pilot ? (d.pilot.pilot_suitability === "RECOMMENDED" ? badge("recommended") : badge(String(d.pilot.pilot_suitability || "").toLowerCase())) : ""}${d.pilot?.ai_reply_eligibility === "ELIGIBLE" ? badge("ready_manual_reply") : ""}`) +
    workflowHero(d.workflow, c, d.ai) +
    `<div class="conversation-refresh"><span>Local data</span>${d.refreshing ? '<span class="muted">Refreshing from HH…</span>' : '<span class="success">Up to date</span>'}<button data-action="sync-conversation" data-id="${esc(id)}">Refresh this conversation</button></div>${localFreshness(d.freshness)}<div class="detail-grid"><div>${messages(c)}${panel("Conversation memory", `<div class="panel-body"><h3>AI summary · сохранённые факты диалога</h3>${ul((d.context?.conversation_summary || c.summary)?.important_facts)}<h3>Обсуждённые темы</h3>${ul((d.context?.conversation_summary || c.summary)?.topics_discussed)}<h3>Открытые вопросы</h3>${ul((d.context?.conversation_summary || c.summary)?.pending_questions)}${ul(d.context?.unresolved_questions)}<h3>Consistency warnings</h3>${ul(d.context?.consistency_warnings)}<p class="muted">Содержание переписки не является подтверждением фактов о кандидате.</p></div>`)}${d.pilot ? panel("Controlled HH Reply Pilot", `<div class="panel-body">${d.pilot.pilot_suitability === "RECOMMENDED" ? `<p>${badge("recommended")} Выберите этот диалог для первого ручного pilot.</p>` : `<p>${badge(String(d.pilot.pilot_suitability || "").toLowerCase())}</p>`}${ul(d.pilot.suitability_reasons)}</div>`) : ""}</div><div>${assistantPanel(d.ai, id, d.context, c, d.context_warning)}${knowledgeContext(d.context?.candidate_context, d.ai?.latest_draft?.used_facts)}</div></div>`
  );
}
const knowledgeTabs = (questions) =>
  `<div class="tabs"><a href="/knowledge" class="${questions ? "" : "active"}">Knowledge base</a><a href="/knowledge/questions" class="${questions ? "active" : ""}">Questions & proposals</a></div>`;
function sourceText(sources) {
  return (
    list(sources)
      .map(
        (s) =>
          `${s.type}${s.reference ? " · " + s.reference : ""}${s.evidence?.length ? " · " + s.evidence.join("; ") : ""}`,
      )
      .join("\n") || "Не указаны"
  );
}
async function knowledge() {
  const d = await api("/knowledge");
  return (
    heading(
      "Candidate knowledge",
      "Проверенные факты, проекты и вопросы, которые помогают AI говорить правду.",
    ) +
    knowledgeTabs(false) +
    panel(
      "Skills",
      list(d.skills).length
        ? `<div class="table-wrap"><table><thead><tr><th>Навык</th><th>Уровень</th><th>Truth status</th><th>Confidence</th><th>Источники</th><th>Проекты</th></tr></thead><tbody>${d.skills.map((s) => `<tr><td>${esc(s.name)}</td><td>${esc(s.level)}</td><td>${badge(s.truth_status)}</td><td>${s.confidence == null ? "—" : esc(Math.round(s.confidence * 100) + "%")}</td><td class="text-block">${esc(sourceText(s.sources))}</td><td>${esc(list(s.projects).join(", ") || "—")}</td></tr>`).join("")}</tbody></table></div>`
        : empty(
            "Навыки пока не добавлены",
            "Здесь появятся данные существующей Candidate Knowledge Base.",
            "",
          ),
    ) +
    panel(
      "Projects",
      list(d.projects).length
        ? `<div class="panel-body knowledge-cards">${d.projects
            .map(
              (p) =>
                `<article class="knowledge-card"><h3>${esc(p.name)}</h3>${badge(p.truth_status)}<p>${esc(p.description)}</p>${pairs(
                  [
                    ["Роль", p.role],
                    ["Технологии", list(p.technologies).join(", ")],
                  ],
                )}<h3>Результаты</h3>${ul(p.results)}<details><summary>Источники</summary><p class="text-block">${esc(sourceText(p.sources))}</p></details></article>`,
            )
            .join("")}</div>`
        : empty(
            "Проекты не добавлены",
            "Неподтверждённые проекты не будут представлены как опыт.",
            "",
          ),
    ) +
    panel(
      "Achievements",
      list(d.achievements).length
        ? `<div class="panel-body knowledge-cards">${d.achievements.map((a) => `<article class="knowledge-card"><h3>${esc(a.title)}</h3>${badge(a.truth_status)}<p>${esc(a.problem)}</p>${ul(a.result)}</article>`).join("")}</div>`
        : empty(
            "Достижения не добавлены",
            "Сохранённые достижения появятся здесь.",
            "",
          ),
    ) +
    panel(
      "Unknowns & proposals",
      `<div class="panel-body"><p>Неизвестные сведения: ${list(d.unknowns).length} · Proposals: ${list(d.proposals).length}</p><a href="/knowledge/questions">Открыть вопросы и предложения →</a></div>`,
    )
  );
}
function answerForm(kind, id, clarification) {
  const shape = clarification?.suggested_answer_shape;
  if (shape?.kind === "choice" && shape.options?.length) {
    return `<form data-answer="${kind}" data-id="${esc(id)}"><input type="hidden" name="answer_kind" value="choice"><label>Выберите точный вариант<select name="choice_id" required><option value="">Выберите…</option>${shape.options.map((o) => `<option value="${esc(o.id)}">${esc(o.label)}</option>`).join("")}</select></label><div class="actions"><button type="submit" class="primary">Сохранить выбор</button><span class="muted">Choice применяется только после явного выбора</span></div></form>`;
  }
  return `<form data-answer="${kind}" data-id="${esc(id)}"><label>Ваш ответ<textarea name="answer" required maxlength="6000" placeholder="Опишите только известные вам факты"></textarea></label><div class="actions"><button type="submit" class="primary">Сохранить ответ</button><span class="muted">Останется неподтверждённым уточнением</span></div></form>`;
}
function proposalValue(p) {
  const v = p.proposed_value || {};
  return `<div class="proposal-value">${pairs([
    ["Тип", p.entity_type],
    ["Название / вопрос", v.name || v.title || v.question],
    ["Уровень", v.level],
    ["Описание / гипотеза", v.description || v.hypothesis],
    ["Источник", p.source],
  ])}${v.can_do?.length ? ul(v.can_do) : ""}${v.result?.length ? ul(v.result) : ""}<details><summary>Полное предлагаемое изменение</summary><pre class="text-block">${esc(JSON.stringify(v, null, 2))}</pre></details></div>`;
}
async function questions() {
  const d = await api("/knowledge/questions");
  return (
    heading(
      "Вопросы к вам",
      "Ваш ответ сохраняется как уточнение. Подтверждение готового proposal — отдельное действие.",
    ) +
    knowledgeTabs(true) +
    panel(
      "Candidate clarifications",
      list(d.clarifications).length
        ? d.clarifications
            .map(
              (c) =>
                `<article class="question"><div class="actions">${badge(c.status)}${c.conversation_id ? link(`/conversations/${urlID(c.conversation_id)}`, "Открыть диалог →") : ""}</div><h3>${esc(c.question)}</h3><p class="muted">${esc(c.reason)}</p>${c.status === "pending" ? answerForm("clarifications", c.id, c) : ""}</article>`,
            )
            .join("")
        : empty(
            "Нет уточнений от AI",
            "AI задаст вопрос, если для ответа работодателю не хватает проверенных данных.",
            "",
          ),
    ) +
    panel(
      "Knowledge proposals",
      list(d.proposals).length
        ? d.proposals
            .map(
              (p) =>
                `<article class="question">${badge(p.status)}<p>${esc(p.reason)}</p>${proposalValue(p)}${p.status === "pending" ? `<div class="actions"><button class="primary" data-action="confirm-proposal" data-id="${esc(p.id)}">Подтвердить эти сведения</button><button data-action="reject-proposal" data-id="${esc(p.id)}">Отклонить</button></div>` : ""}</article>`,
            )
            .join("")
        : empty(
            "Нет предложений на проверку",
            "Готовые proposals из существующего knowledge pipeline появятся здесь.",
            "",
          ),
    ) +
    panel(
      "Candidate unknowns",
      list(d.unknowns).length
        ? d.unknowns
            .map(
              (u) =>
                `<article class="question">${badge(u.status)}<h3>${esc(u.question)}</h3>${u.hypothesis ? `<p class="text-block">${esc(u.hypothesis)}</p><p class="muted">Ответ сохранён; он ещё не стал подтверждённым навыком или фактом.</p>` : ""}${u.status === "needs_confirmation" ? answerForm("unknowns", u.id) : ""}</article>`,
            )
            .join("")
        : empty(
            "Неизвестных сведений пока нет",
            "При нехватке информации здесь появятся вопросы.",
            "",
          ),
    )
  );
}
async function analytics() {
  const period = new URLSearchParams(location.search).get("period") || "all";
  const d = await api(`/analytics?period=${encodeURIComponent(period)}`);
  const max = Math.max(
    1,
    ...d.daily.flatMap((v) => [v.applications, v.replies]),
  );
  return (
    heading("Analytics", "Понимайте, как движется ваш поиск работы.") +
    `<form class="filter-bar" data-filter>${select(
      "period",
      "Период",
      [
        ["today", "Сегодня"],
        ["7d", "7 дней"],
        ["30d", "30 дней"],
        ["all", "Всё время"],
      ],
      period,
    )}<button>Показать</button></form>` +
    metricsCards(d.metrics) + followUpMetrics(d.follow_up_analytics) +
    `<div class="stats"><div class="stat"><div class="stat-label">Response rate</div><div class="stat-value">${d.metrics.response_rate.toFixed(1)}%</div></div><div class="stat"><div class="stat-label">Interview conversion</div><div class="stat-value">${d.metrics.interview_conversion.toFixed(1)}%</div></div></div>` +
    panel(
      "Отклики и первые ответы по дням",
      d.daily.some((v) => v.applications || v.replies)
        ? `<div class="chart"><div class="chart-bars">${d.daily.map((v) => `<div class="chart-day"><div>${v.applications} / ${v.replies}</div><div class="bars"><meter min="0" max="${max}" value="${v.applications}" aria-label="${esc(v.date)}: ${v.applications} откликов"></meter><meter class="reply" min="0" max="${max}" value="${v.replies}" aria-label="${esc(v.date)}: ${v.replies} ответов"></meter></div><span>${esc(v.date.slice(5))}</span></div>`).join("")}</div><div class="legend"><span><i></i>Отклики</span><span><i class="green"></i>Первые ответы</span></div></div>`
        : empty(
            "Пока недостаточно данных для графика",
            "График строится только по известным датам откликов и сообщений.",
            "",
          ),
    ) +
    `<div class="callout"><strong>Как считаются метрики</strong>Response rate — доля откликов выбранного периода с сохранённым ответом работодателя. Interview conversion — доля с зафиксированным интервью. Отклики без достоверной даты входят только в «Всё время». Отказ сам по себе не считается сообщением работодателя. График учитывает даты откликов и первых ответов, а очереди AI и знаний показывают текущее состояние.</div>`
  );
}
function reliabilityState(item) {
  return `<div><strong>${esc(item.state)}</strong><br><span class="muted">${esc(item.display_label)}</span></div>`;
}
function reliabilityApplicationTable(items) {
  return items.length
    ? `<div class="table-wrap"><table><thead><tr><th>State</th><th>Vacancy</th><th>Attempt ID</th><th>Resume</th><th>Provider evidence</th><th>Updated</th><th>Reason</th></tr></thead><tbody>${items.map((item) => `<tr><td>${reliabilityState(item)}</td><td>${esc(item.vacancy_id)}</td><td>${link(`/reliability/application-attempts/${urlID(item.attempt_id)}`, item.attempt_id)}</td><td>${esc(item.resume_id)}</td><td>${esc(item.provider_application_id || item.provider_negotiation_id || "none")}</td><td>${date(item.updated_at)}</td><td>${esc(item.reason || "—")}</td></tr>`).join("")}</tbody></table></div>`
    : empty("Заявок для просмотра нет", "В выбранном bounded-фильтре нет durable application attempts.", "");
}
function reliabilityAutoChatTable(items) {
  return items.length
    ? `<div class="table-wrap"><table><thead><tr><th>State</th><th>Conversation</th><th>Trigger message</th><th>Action</th><th>Attempt ID</th><th>Outgoing provider ID</th><th>Updated</th><th>Reason</th></tr></thead><tbody>${items.map((item) => `<tr><td>${reliabilityState(item)}</td><td>${esc(item.conversation_id)}</td><td>${esc(item.trigger_message_id)}</td><td>${esc(item.action_type)}</td><td>${link(`/reliability/autochat-attempts/${urlID(item.attempt_id)}`, item.attempt_id)}</td><td>${esc(item.provider_outgoing_message_id || "none")}</td><td>${date(item.updated_at)}</td><td>${esc(item.reason || "—")}</td></tr>`).join("")}</tbody></table></div>`
    : empty("Auto-chat попыток для просмотра нет", "В выбранном bounded-фильтре нет durable auto-chat attempts.", "");
}
async function reliabilityPage() {
  const query = new URLSearchParams(location.search);
  const suffix = query.toString() ? `?${query}` : "";
  const [applications, autochat] = await Promise.all([
    api(`/reliability/application-attempts?needs_attention=true${suffix ? `&${query}` : ""}`),
    api(`/reliability/autochat-attempts?needs_attention=true${suffix ? `&${query}` : ""}`),
  ]);
  return heading("Reliability", "Bounded read-only inspection of durable automatic attempts.") +
    panel("Требуют внимания · applications", `<div class="panel-body"><p class="muted">${esc(applications.store?.status || "ERROR")} · backend ${esc(applications.store?.backend || "—")}</p>${reliabilityApplicationTable(list(applications.items))}</div>`) +
    panel("Требуют внимания · legacy auto-chat", `<div class="panel-body"><p class="muted">${esc(autochat.store?.status || "ERROR")} · backend ${esc(autochat.store?.backend || "—")}</p>${reliabilityAutoChatTable(list(autochat.items))}</div>`);
}
async function reliabilityDetail(kind, id) {
  const d = await api(`/reliability/${kind}/${urlID(id)}`);
  const a = d.attempt || {};
  const values = kind === "application-attempts"
    ? [["Attempt ID", a.attempt_id], ["Vacancy", a.vacancy_id], ["Resume", a.resume_id], ["Technical state", a.state], ["Classification", a.classification], ["Display", a.display_label], ["Provider application ID", a.provider_application_id], ["Provider negotiation ID", a.provider_negotiation_id], ["Evidence", a.evidence_kind], ["Evidence source", a.evidence_source], ["Observed", date(a.observed_at)], ["Created", date(a.created_at)], ["Updated", date(a.updated_at)], ["Reason", a.reason], ["Causality", a.causality_note]]
    : [["Attempt ID", a.attempt_id], ["Conversation", a.conversation_id], ["Trigger message", a.trigger_message_id], ["Action", a.action_type], ["Technical state", a.state], ["Classification", a.classification], ["Display", a.display_label], ["Request key", a.provider_request_key], ["Outgoing provider ID", a.provider_outgoing_message_id], ["Created", date(a.created_at)], ["Updated", date(a.updated_at)], ["Reason", a.reason], ["Causality", a.causality_note]];
  return heading("Reliability detail", "Read-only durable attempt inspection.", link("/reliability", "← Reliability")) + panel("Attempt", pairs(values));
}
function syncResult(result) {
  if (!result) return "";
  const values =
    result.fetched !== undefined ? [["Sync", result]] : Object.entries(result);
  return panel(
    "Последний SyncResult",
    `<div class="table-wrap"><table><thead><tr><th>Раздел</th><th>Fetched</th><th>Created</th><th>Updated</th><th>Unchanged</th><th>Skipped</th><th>Errors</th></tr></thead><tbody>${values.map(([name, v]) => `<tr><td>${esc(name)}</td>${["fetched", "created", "updated", "unchanged", "skipped"].map((k) => `<td>${v[k] ?? 0}</td>`).join("")}<td>${list(v.errors).length}</td></tr>`).join("")}</tbody></table></div><div class="panel-body">${values.map(([name, v]) => (list(v.errors).length || list(v.warnings).length ? `<h3>${esc(name)}</h3>${ul(v.errors)}${ul(v.warnings)}` : "")).join("")}</div>`,
  );
}
async function syncPage() {
  const d = await api("/sync/status");
  const s = d.state;
  syncWatching = d.running;
  return (
    heading(
      "HH Sync",
      "Обновите локальное представление поиска. Синхронизация выполняет только чтение HH.",
    ) +
    panel(
      "Maintenance / Advanced",
      `<div class="panel-body">${badge(d.running ? "pending" : "no_reply_needed")}<p>${d.running ? "Синхронизация выполняется…" : "Обычная работа не требует Full Sync."}</p>${pairs(
        [
          ["Last vacancy sync", date(s.last_vacancy_sync_at)],
          ["Last application sync", date(s.last_application_sync_at)],
          ["Last conversation sync", date(s.last_conversation_sync_at)],
          ["Last Inbox refresh", date(s.last_inbox_refresh_at)],
          ["Last success", date(s.last_success)],
          ["Last error", s.last_error || "Нет сохранённой ошибки"],
        ],
      )}<p class="muted">Full verification — may take ~5–6 minutes. Нужен для глубокой reconciliation, но не перед draft, preflight или send.</p><div class="actions">${[
        ["all", "Sync Everything"],
        ["vacancies", "Full Sync Vacancies"],
        ["applications", "Full Sync Applications"],
        ["conversations", "Full Sync Conversations"],
      ]
        .map(
          ([id, text]) =>
            `<button data-action="sync" data-id="${id}" class="${id === "all" ? "primary" : ""}" ${d.running ? "disabled" : ""}>${text}</button>`,
        )
        .join("")}</div></div>`,
    ) +
    `<div id="sync-progress">${syncProgress(d)}</div>` +
    syncResult(d.last_result) +
    `<div class="callout"><strong>Обновление интерфейса и HH sync — разные действия</strong>Интерфейс проверяет изменения локальных данных. Sync this conversation и fresh preflight обновляют один диалог; полный sync запускается отдельно.</div>`
  );
}
async function render(silent = false) {
  if (loading) return;
  loading = true;
  const request = ++currentRequest;
  const path = location.pathname;
  const base = "/" + path.split("/")[1];
  const title = titles[path] || titles[base] || "Conversation";
  document.getElementById("breadcrumb").textContent = title;
  document.title = `${title} · Career Agent`;
  document.querySelectorAll("[data-nav]").forEach((a) => {
    const active =
      a.dataset.nav === "/"
        ? path === "/"
        : path.startsWith(a.dataset.nav) ||
          (a.dataset.nav === "/inbox" && base === "/conversations");
    a.classList.toggle("active", active);
    if (active) a.setAttribute("aria-current", "page");
    else a.removeAttribute("aria-current");
  });
  try {
    // Share this render-cycle dashboard read between the global indicator and
    // Overview. The backend protects local stores with a single-operation
    // lock, so only independent Overview reads are launched concurrently.
    const dashboardPromise = api("/dashboard");
    await Promise.all([
      updateNotificationIndicator(dashboardPromise),
      updateGlobalSyncStatus(),
    ]);
    let html;
    const parts = path.split("/").filter(Boolean);
    if (path === "/") html = await overview(dashboardPromise);
    else if (path === "/today") html = await today();
    else if (path === "/applications") html = await applications();
    else if (path === "/vacancies") html = await vacancies();
    else if (path === "/career") html = await careerAgentWorkspace();
    else if (path === "/inbox") html = await inbox();
    else if (path === "/knowledge") html = await knowledge();
    else if (path === "/knowledge/questions") html = await questions();
    else if (path === "/analytics") html = await analytics();
    else if (path === "/sync") html = await syncPage();
    else if (path === "/health") html = await healthPage();
    else if (path === "/reliability") html = await reliabilityPage();
    else if (parts.length === 2 && parts[0] === "applications")
      html = await applicationDetail(decodeURIComponent(parts[1]));
    else if (parts.length === 2 && parts[0] === "vacancies")
      html = await vacancyDetail(decodeURIComponent(parts[1]));
    else if (parts.length === 2 && parts[0] === "conversations")
      html = await conversationDetail(decodeURIComponent(parts[1]));
    else if (parts.length === 3 && parts[0] === "reliability")
      html = await reliabilityDetail(parts[1], decodeURIComponent(parts[2]));
    else throw new Error("Страница не найдена");
    if (request === currentRequest) {
      main.innerHTML = html;
      document.getElementById("refresh-status").textContent =
        `Локальные данные · ${new Date().toLocaleTimeString("ru-RU")} · refresh 45s`;
    }
  } catch (err) {
    if (silent) {
      document.getElementById("refresh-status").textContent =
        `Обновление отложено: ${err.message}`;
    } else {
      main.innerHTML =
        heading(title, "") +
        `<div class="error"><h2>Не удалось загрузить данные</h2><p>${esc(err.message)}</p><button data-action="reload">Попробовать снова</button></div>`;
    }
  } finally {
    loading = false;
  }
}
document.addEventListener("submit", async (event) => {
  const form = event.target;
  if (form.matches("[data-filter]")) {
    event.preventDefault();
    const q = new URLSearchParams(new FormData(form));
    for (const [key, value] of [...q]) if (!value) q.delete(key);
    location.search = q.toString();
    return;
  }
  if (!form.matches("[data-answer]")) return;
  event.preventDefault();
  if (busy) return;
  busy = true;
  const button = form.querySelector("button");
  button.disabled = true;
  try {
    const formData = new FormData(form);
    const payload = Object.fromEntries(formData.entries());
    if (payload.answer_kind === "choice") {
      const selected = form.querySelector("select[name=choice_id] option:checked");
      payload.answer = selected?.textContent || payload.choice_id;
    }
    await api(
      `/knowledge/${form.dataset.answer}/${urlID(form.dataset.id)}/answer`,
      payload,
    );
    toast("Ответ сохранён как неподтверждённое уточнение.");
    await render();
  } catch (err) {
    toast(err.message);
  } finally {
    busy = false;
    button.disabled = false;
  }
});
document.addEventListener("click", async (event) => {
  const button = event.target.closest("[data-action]");
  if (!button) return;
  event.preventDefault();
  const { action, id } = button.dataset;
    if (action === "reload") {
    await render();
      return;
    }
    if (action === "career-daily-run") {
      if (busy) return;
      busy = true;
      button.disabled = true;
      try {
        const result = await api("/career/run", {});
        toast(`Daily run: ${result.summary?.result || result.run?.status || "completed"}`);
        await render();
      } catch (err) {
        toast(err.message);
      } finally {
        busy = false;
        button.disabled = false;
      }
      return;
    }
    if (action === "deep-health") {
      main.innerHTML = await healthPage(true);
      return;
    }
    if (action === "fast-health") {
      main.innerHTML = await healthPage(false);
      return;
    }
  if (action === "copy") {
    try {
      await navigator.clipboard.writeText(draftsForCopy.get(id) || "");
      toast("Черновик скопирован");
    } catch {
      toast("Не удалось скопировать. Выделите текст черновика вручную.");
    }
    return;
  }
  if (action === "notification-open") {
    try { await api(`/notifications/${urlID(id)}/open`, {}); } catch {}
    location.href = button.getAttribute("href");
    return;
  }
  if (busy) return;
  busy = true;
  button.disabled = true;
  const previous = button.textContent;
  button.textContent = "Выполняется…";
  try {
    if (action === "vacancy-review") {
      const reason = button.dataset.review === "dismiss" ? window.prompt("Причина скрытия (необязательно)") || "" : "";
      await api(`/vacancies/${urlID(id)}/review/${button.dataset.review}`, { reason });
      toast(button.dataset.review === "interesting" ? "Вакансия отмечена интересной" : button.dataset.review === "dismiss" ? "Вакансия скрыта из активной очереди" : "Вакансия отмечена просмотренной");
    } else if (action === "follow-up-draft" || action === "follow-up-dismiss") {
      const d = await api(`/follow-ups/${urlID(id)}/${action === "follow-up-draft" ? "draft" : "dismiss"}`, {});
      toast(action === "follow-up-dismiss" ? "Предложение скрыто локально" : (statuses[d.action]?.[0] || d.action));
      if (action === "follow-up-draft" && d.action === "draft_reply") { location.href = `/applications/${urlID(id)}`; }
    } else if (action === "classification-correct") {
      await api(`/conversations/${urlID(id)}/classification-feedback`, { correct: true });
      toast("Classification feedback saved");
    } else if (action === "classification-wrong") {
      const select = document.querySelector(`[data-classification-state="${CSS.escape(id)}"]`);
      await api(`/conversations/${urlID(id)}/classification-feedback`, { correct: false, state: select?.value || "other" });
      toast("Classification correction saved for review");
    } else if (action === "conversation-irrelevant") {
      await api(`/conversations/${urlID(id)}/irrelevant`, {});
      toast("Скрыто до нового сообщения или изменения состояния");
    } else if (action === "notification-ack") {
      await api(`/notifications/${urlID(id)}/ack`, {});
      toast("Уведомление скрыто локально");
    } else if (action === "notification-snooze") {
      await api(`/notifications/${urlID(id)}/snooze`, { until: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString() });
      toast("Напоминание отложено до завтра");
    } else if (action === "draft") {
      const d = await api(`/conversations/${urlID(id)}/draft`, {});
      toast(statuses[d.action]?.[0] || d.action);
    } else if (action === "reject") {
      await api(`/drafts/${urlID(id)}/reject`, {});
      toast("Черновик отклонён локально");
    } else if (action === "edit-draft") {
      const editor = document.querySelector(`[data-draft-editor="${CSS.escape(id)}"]`);
      if (!editor) throw new Error("Редактор черновика не найден");
      await api(`/drafts/${urlID(id)}/edit`, { text: editor.value });
      toast("Изменённый текст сохранён и повторно проверен");
    } else if (action === "draft-feedback-good") {
      await api(`/drafts/${urlID(id)}/feedback`, { accepted: true });
      toast("Draft feedback saved");
    } else if (action === "draft-feedback-edit") {
      const editor = document.querySelector(`[data-draft-editor="${CSS.escape(id)}"]`);
      const reason = document.querySelector(`[data-draft-reason="${CSS.escape(id)}"]`)?.value || "другое";
      if (!editor) throw new Error("Редактор черновика не найден");
      await api(`/drafts/${urlID(id)}/feedback`, { accepted: false, edited_text: editor.value, reason_category: reason });
      toast("Пример исправления сохранён локально");
    } else if (action === "approve-draft") {
      await api(`/drafts/${urlID(id)}/approve`, { approved_by: "dashboard_user" });
      toast("Точный текст подтверждён. Проверьте Ready to send и нажмите Send отдельно.");
    } else if (action === "preflight-action") {
      const result = await api(`/actions/${urlID(id)}/preflight`, {});
      toast(result.allowed ? "READY_TO_SEND: свежий preflight пройден" : `BLOCKED: ${(result.reasons || []).join("; ")}`);
    } else if (action === "review-action") {
      const current = button.closest(".callout");
      toast(current?.innerText || "Проверьте изменения Candidate Knowledge и approved text перед повторным approval.");
    } else if (action === "send-action") {
      toast("Sending…");
      const result = await api(`/actions/${urlID(id)}/send`, {
        nonce: button.dataset.nonce || "",
        ui_event: "send_ui_clicked",
      });
      toast(result.status === "delivery_confirmed" ? "Sent to HH · Delivery confirmed" : (result.error || "Sent to HH · Delivery uncertain"));
    } else if (action === "reconcile-action") {
      const result = await api(`/actions/${urlID(id)}/reconcile`, {});
      toast(result.status === "delivery_confirmed" ? "Доставка подтверждена read-only sync" : (result.error || "Доставка пока не подтверждена"));
    } else if (action === "cancel-action") {
      await api(`/actions/${urlID(id)}/cancel`, {});
      toast("Отправка отменена локально");
    } else if (action === "sync" || action === "sync-conversation") {
      const path = action === "sync-conversation" ? `/conversations/${urlID(id)}/sync` : `/sync${id === "all" ? "" : "/" + id}`;
      const result = await api(path, {}, true);
      syncWatching = result.running;
      toast(result.message || "Read-only sync запущен; можно продолжать работу.");
    } else if (action === "confirm-proposal" || action === "reject-proposal") {
      await api(
        `/knowledge/proposals/${urlID(id)}/${action === "confirm-proposal" ? "confirm" : "reject"}`,
        {},
      );
      toast(
        action === "confirm-proposal"
          ? "Предложение подтверждено"
          : "Предложение отклонено",
      );
    }
    await render();
  } catch (err) {
    toast(action === "send-action" ? sendFailureMessage(err) : err.message);
  } finally {
    busy = false;
    button.disabled = false;
    button.textContent = previous;
  }
});
// UI polling never triggers HH sync, and never erases an answer being edited.
setInterval(() => {
  if (
    document.hidden ||
    busy ||
    loading ||
    (main.contains(document.activeElement) &&
      document.activeElement.matches("input,textarea,select")) ||
    [...main.querySelectorAll("[data-answer] textarea")].some((el) => el.value)
  )
    return;
  api("/generation").then((d) => { if (lastGeneration !== d.generation) { lastGeneration = d.generation; render(true); } }).catch(() => {});
}, 45000);
render();

function syncProgress(d) {
  const rows = list(d.progress).map((p) => `<p>${esc(p.target.split(":")[0])}: ${p.processed} / ${p.fetched} · Changed: ${p.changed} · Unchanged: ${p.unchanged} · History reused: ${p.history_reused || 0} · Detailed chats: ${p.detailed_chats_fetched || 0} · Errors: ${p.errors} · Requests: ${p.requests || 0} · ${(p.requests_per_second || 0).toFixed(2)} req/s</p>`).join("");
  return panel("Sync progress", `<div class="panel-body">${rows || '<p class="muted">No sync operation running.</p>'}<p>Elapsed: ${Math.round((d.elapsed_ms || 0) / 1000)} s</p></div>`);
}
setInterval(async () => {
  updateGlobalSyncStatus();
  if (!syncWatching || document.hidden) return;
  try {
    const d = await api("/sync/status");
    const node = document.getElementById("sync-progress");
    if (node) node.innerHTML = syncProgress(d);
    if (!d.running) {
      syncWatching = false;
      toast("Sync завершён. Локальные данные обновлены.");
      if (!busy && !loading && !main.querySelector("textarea:focus,input:focus")) await render(true);
    }
  } catch {}
}, 2000);
