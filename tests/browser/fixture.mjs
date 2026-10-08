// A made-up CV, long enough to run over two pages, in English and Dutch.

import { api } from "./lib.mjs";

const bullets = (topic) =>
  [
    `- Led a team of five on ${topic}, from first sketch to daily use.`,
    `- Wrote the guidelines that the department still follows for ${topic}.`,
    "- Presented results to management every quarter.",
    "- Mentored two interns and one new colleague.",
    "- Kept the budget, planning and supplier contracts in order.",
    `- Gave workshops on ${topic} to other teams and partner organisations.`,
  ].join("\n");

const jobs = [
  ["2022-03", "", "Harbour Analytics Lead", "Port of Exampletown", "lighthouse data"],
  ["2019-01", "2022-02", "Mapmaker", "Atlas & Sons", "coastal maps"],
  ["2016-09", "2018-12", "Clockwork Engineer", "Tick Tock Ltd", "tower clocks"],
  ["2014-02", "2016-08", "Beekeeping Consultant", "Hive Mind Cooperative", "urban hives"],
  ["2012-06", "2014-01", "Ferry Scheduler", "Northern Ferries", "winter timetables"],
  ["2010-09", "2012-05", "Library Assistant", "Exampletown Library", "rare books"],
];

const studies = [
  ["2008-09", "2010-07", "MSc Navigation", "University of Exampletown"],
  ["2005-09", "2008-07", "BSc Astronomy", "Example Institute of Technology"],
];

const papers = [
  ["2024", "Tides and timetables", "Example, A., & Doe, J. (2024). Tides and timetables. *Journal of Made-up Results*, 12(3), 45–67."],
  ["2021", "Lighthouses at night", "Example, A. (2021). Lighthouses at night. *Coastal Notes*, 4, 1–9."],
  ["", "Bees in the city", "Example, A., & Roe, R. (in review). Bees in the city. *Urban Ecology Letters*."],
];

export async function seed(base) {
  await api(base, "PUT", "/api/profile", {
    name: "Alice Example",
    email: "alice@example.com",
    phone: "+31 6 12345678",
    website: "https://example.com",
    links: [{ label: "LinkedIn", url: "https://www.linkedin.com/in/alice-example" }],
    text: {
      en: { headline: "Harbour analyst", location: "Exampletown, the Netherlands", summary: "Analyst who turns *sea charts* into decisions." },
      nl: { headline: "Havenanalist", location: "Exampletown, Nederland", summary: "Analist die *zeekaarten* omzet in beslissingen." },
    },
  });
  for (const [start, end, title, org, topic] of jobs) {
    await api(base, "POST", "/api/items", {
      section: "experience",
      start,
      end,
      text: {
        en: { title, org, location: "Exampletown", body: bullets(topic) },
        nl: { title: title + " (NL)", org, location: "Exampletown", body: bullets(topic) },
      },
    });
  }
  for (const [start, end, title, org] of studies) {
    await api(base, "POST", "/api/items", { section: "education", start, end, text: { en: { title, org } } });
  }
  for (const [start, title, body] of papers) {
    await api(base, "POST", "/api/items", { section: "publications", start, text: { en: { title, body } } });
  }
  await api(base, "POST", "/api/items", {
    section: "presentations",
    start: "2023-05",
    text: { en: { title: "Counting ships", org: "Ship Days 2023" } },
  });
  for (const [start, end, title] of [
    ["2020-09", "2023-06", "Course in Sea Charts"],
    ["2018-02", "2020-06", "Seminar on Tides"],
    ["2015-09", "2017-06", "Workshop Knots for Beginners"],
  ]) {
    await api(base, "POST", "/api/items", {
      section: "teaching",
      start,
      end,
      text: { en: { title, org: "Exampletown Academy", body: bullets("the course") } },
    });
  }
}
