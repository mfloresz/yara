// cherrymist.cafe — a client-rendered React SPA whose routes all return the same
// empty <div id="root"> shell, so everything comes from the JSON API under /api.
const BASE = "https://cherrymist.cafe";
const API = BASE + "/api";

// Index i of each table is the character for U+E000+i. Seeds are 0..15; the
// site ships exactly one font per seed in that range and the permutation is
// identical across the regular/bold/italic faces of a seed.
const CIPHER_TABLES = [
  "lrtjttdQRofisoelnohosassbwehJihzaBhoutilafaiobetneZmwGttPoIUaoarenOLrkeertrCeilaaMhlfvetuothXtqneieesswgcKTbsnctepisuicngdaaVrnsltesnmtxwhrigErdoiprhlerfeodeciAcdaenmahgotuNnateeddaklgymottcfrpnehessryalDnrsmpohteamiyounuvnhiSaFiohWahteiaosYnweddnrdeHiyyst",
  "etcasvneCaocwebotclilJheitVsrodtKFMgyfselnmeowkaohtzWeOhruestsmlaiBrtoiZulrhnqnforfoomLiayheesnusinQiipfijhdTaEtgtetntRmoPoGnwieundIiirhydpetdmatstUdroetdtaadYlleerewhrhAedNHacetehnynotsrahhhavDiernihaSubeXercfamrgaxsasoosneksygdeuaipcpbritnastgowlalentons",
  "CtJccIudjfkasgpqaungdttaxgodfiltsndrtQpglseBcotdeEsnncthhenetittFVweYeloinnpDateGlnsnaoanedldhcAnfaweenahyhekiuarpdeoZthtallrKbtfssetevenaubaiehewdwXliossmriumsnywtofiaosLyynrhoNsiiOtnishrrmzHrtmiMoeebuhotleerTayahrsmdhoeaUrWhaPvocgseoitroooeiSitRhiaarreem",
  "AtlKohadlflaeavaeavRyunonnhnussnJroQiinTtetLgarisdsoaoDabrOcoVwqecdiftnoepSseeNtmailaooeisPusmcWsoinFtgMryeXrtaidywemetbdnahhhgUpErhinneeophztitorYsofeoylekwoHkeaaedhomhruwbcfeuntGpreeitliwlrIcetatyeemgnfatinirsdmxdhaajtssnthZdedertsgeitsrBCuhnlthtchoarlsi",
  "ntPtatngHshhatbrmtkpiaroQvylaoeissnfblhmsefraFVstdIiaterwotmetedarnooOheneysahGwplkjmwnetcJsenlsartcnhtUolaigiYsolnoaebatthpdheotnsomAitadaSfiryodidrgneuesLafdXeEnheirxMZropoednenlcdWerfwiemhorhBcretiiauaeoqcngwzRtsCieteTlNuoauuthicsDsuryKsyoeeidvehnhliagt",
  "acarMsatsnraeuprHtyttohluesrsavayhrensidttewehhfoGlFBNnWmuCosLkItiitkeernoeYeoniidrystsmrpgctmbtsannuSaedopeeejVTuchcniealhesimOZeanrigllrctdgnrhbhalimdaPahqthigncrynReodlohossedoettoiyQUnfetxnaiwttJiDlnootfebrevmwlofaarpsdXiteKdiAsgewefEhhedontaoawzaihosu",
  "gneriilienCacloynmnhshafhsnhiatclcabsBmhooLDstghoeneyarpafnlOrnsJtathtemoioericeeSmrthepHdshcomlbieoqhnuywoefigIhicteiavdotmUisirdenpeaaQGefdettaFootnsnoretnvAkKeYrjufilneipsdtryauTdtaubolguozEeRxtekldortaunhehestswaswrZaoadnhtaVgtresielNwerMrXsWydwdstitaP",
  "ofrirridaotDsgsenpldrfdlrthitrelcnsiohugshtkneHsdonwoenSoogBuaiahesenKspyaeorgaRutdhohbeeanFotdiwsOhigicsnpsnhnVheyttlketersrdpesndZoaWtsrhcniaoeweerjtfXvdyUeccfaCzLmnnmuttMaomrittemotuixawheGabvQAattoinysymetaoroYfieIhhrmatiNTulbelqaasthielaweJaldcPinEeel",
  "wletotniraytoaqrnvliolgrkaueLyVthashtthgCtlflnrtdZregtthclscthterusrtdfefibresiacenJhuSepednomotwttyatiHeespanQxuOrmhainehhheGuworkebdovshaodaecmoewfiraeDhndieseThtXUgdrainoIecjoiWpoaefiAnydeybsPessnismeapcnumaeKrEtaYstoiatrinlnooBddoRswsnmasozFigeaNihllnM",
  "ekalraolooyuemnCvtljsnsrybVslnLcYrfMXlatremiaaDdJnpeEhiewdaeGOcgtwmantoaledstslaQhIbngidetrhtusethnrelidoqiaydisatoipnfaehteweohssurthxdesdkiemoeheweaauigFstmiheelcRormffursnaTieoaectoetsnStobortBhhcrfWscieoawhytodZnsttvnoonheainagzHrreinhpitnKNpPtigAryduU",
  "tsEunattoopphingisfavlKsijxcdthitloaedfoZhntBonthemcsuoayilwhRhfwhtnnsdDVtarthrugneOslenymtbteanlrssefaoimeQFnirpbfeshHbrpcarrdirirPiAoaScrsmsandthiaiowGaMvalLoauehaehcetIeydgoaeiagaTrogezenqrcemXdedtiYldoeiehishsweWeeokotntynewttJdraNenoutneeCtousmslkrlyU",
  "intheZredADelneGreocfaeptateeeohIeftmrngrewyuekhXdctCgpgnheingsonaicnslhhdweoosztelioablataaSsotHUPsatOsiytlYnMnreiRwcrsmntstrierJeuhdsaxahhfetbLerlrmnauiyqdlyiwobasttowNEiedshhofodserlkrejruyhfKiieaiiimttWdalsovsdpnFvtBmramnaonsdQottogoccnhtpaVinuaaueoheT",
  "hotedemlooWeahJiayvhyolpfVhihfhNnahrymgletnesonhqlrioanmgGdnmAieetteeasuaYalersevededutdwzrHmtpIeutacothrlbtRawnacednelosUgthriesoeiguPcanparnydhwKdjsunCifsoaBXfabaoiowlhcQeLtstnineehsttdcstterahasrrerMknattnioowislOoZbeFEysniosrfdirgtneTitcmSisioterDkaxpu",
  "odMsGtmtKdwdNTagnawctdraohhlabtmnrjoJsnlaezDeidneeiuotkawnotHedhlehoyebsefisncraXpanrnoetiteerlrenlegmerdesxiepebdorwhnBotusZchnsgrfoenduidstichaaqteAmaoWPhleVtnoiaattoQoeuemtrahutsrthifvsssavkengrsnnpleiargspoftFihaOhhlEtSsheltuyCiaitcYaUoIyiowRiymLrefyic",
  "etpictwedrniuoJhureoiRPtIsosrTinsuoEbchorleZeexAndadoauisctsnoHnMolmKenwvrehUtetiBgelgphdahataotfnasYiayvamLoiahsWfjeeltggituOltpuenhrreobeicnaafetwktnohefnshhtyipedgnQesneitkmcsdGyqhsesaoroaaoNsacaSeyzsdmaitFhdttfnmnmdhsryrhCliarirrwteeditelDwlonoeaVbXlrt",
  "ooayeicoeaeocThwcoselxmhtitshdeeFsknudiieaLOlEteopiaaYtnhidjneyiebRsftocucmnseoanantaaQwdfhGsnPbogpVeiiesWndrelihrptZduKtSuwagCegarDsemliorihyatraieeodtclIqtogytMsvsrnedshbrtlryJhaieeHfeoAuogtsNedtsiUnwhpltudinawmfoohfleXhrtlvamskaanntrrnnhtrsmnotBrthazeer"
];

const clean = (s) => (s || "").replace(/\s+/g, " ").trim();

function pathParts(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/[^/?#]*(\/[^?#]*)/i.exec(u);
  const raw = m ? m[1] : u;
  return raw.replace(/^\/+|\/+$/g, "").split("/").filter((p) => p !== "");
}

// Supports /story/<series>/, /story/<series>/chapter/<n>/ and /chapter/<slug>/.
function slugs(ctx, rawURL) {
  const parts = pathParts(rawURL);
  let seriesSlug = "";
  let chapterSlug = "";
  for (let i = 0; i < parts.length; i++) {
    if ((parts[i] === "story" || parts[i] === "series" || parts[i] === "novel") && i + 1 < parts.length) {
      seriesSlug = parts[i + 1];
    }
    if (parts[i] === "chapter" && i + 1 < parts.length) {
      chapterSlug = parts[i + 1];
    }
  }
  if (seriesSlug === "" && chapterSlug === "") {
    ctx.fail("not_my_site", rawURL + " is not a cherrymist story or chapter URL");
  }
  return [seriesSlug, chapterSlug];
}

function fetchJSON(ctx, url) {
  return JSON.parse(ctx.get(url).body);
}

function seriesKey(ctx, rawURL) {
  const parts = slugs(ctx, rawURL);
  let key = parts[0];
  if (key !== "") return key;
  // A chapter URL identifies the series only indirectly, via the chapter.
  const ref = chapterRef(ctx, parts[1]);
  if (ref.series_slug) return ref.series_slug;
  return String(ref.series_id);
}

function chapterRef(ctx, slug) {
  const ref = fetchJSON(ctx, API + "/chapters/by-slug/" + encodeURIComponent(slug));
  if (!ref || !ref.id) ctx.fail("site_layout_changed", "chapter " + slug + " not found");
  return ref;
}

function part(p) {
  return p === null || p === undefined ? 0 : p;
}

function authorOf(s) {
  const candidates = [s.original_author, s.author_name, s.translator ? s.translator.name : null];
  for (const c of candidates) {
    if (c && String(c).trim() !== "") return c;
  }
  return "";
}

function chapterRefs(ctx, seriesID) {
  // published=1 drops scheduled chapters that have no readable body yet;
  // without it the endpoint also returns future-dated entries for a finished
  // story, which would fail on the first download.
  const list = fetchJSON(ctx, API + "/chapters?series_id=" + seriesID + "&published=1&limit=500");
  const items = Array.isArray(list) ? list.slice() : [];
  items.sort((a, b) => (a.chapter_number || 0) - (b.chapter_number || 0) || part(a.part_number) - part(b.part_number));

  const refs = [];
  for (const c of items) {
    if (!c.slug) continue;
    refs.push({ title: clean(c.title), url: BASE + "/chapter/" + c.slug });
  }
  if (refs.length === 0) {
    ctx.fail("site_layout_changed", "series " + seriesID + " has no published chapters");
  }
  return refs;
}

// The body ships with its letters remapped onto Private Use Area codepoints;
// everything outside the cipher range is already in the clear.
function decodeBody(s, seed) {
  const table = CIPHER_TABLES[seed];
  if (!table) return s;
  let out = "";
  for (let i = 0; i < s.length; i++) {
    const code = s.charCodeAt(i);
    if (code >= 0xe000 && code <= 0xf8ff) {
      const j = code - 0xe000;
      if (j < table.length) {
        out += table[j];
        continue;
      }
    }
    out += s[i];
  }
  return out;
}

const BLOCK_TAG = /<(p|div|ul|ol|li|h[1-6]|blockquote|table|pre|figure|section|article|hr)[\s>/]/i;

// Mirrors how the site renders the field: unwrap the presentational
// font-weight spans, then treat each blank-line-separated block as a paragraph
// unless it already carries block-level markup.
function paragraphs(content) {
  const cleaned = content.replace(/\r\n/g, "\n").replace(/<\/?span[^>]*>/gi, "");
  const blocks = [];
  for (const raw of cleaned.split(/\r?\n[ \t]*\r?\n/)) {
    const block = raw.trim();
    if (block === "") continue;
    if (BLOCK_TAG.test(block)) {
      blocks.push(block);
      continue;
    }
    blocks.push("<p>" + block.split("\n").join("<br />") + "</p>");
  }
  return blocks.join("\n");
}

// Strict host check: only this site's domain (and its subdomains) is
// claimed, never a URL that merely mentions the domain in a query string.
function isSiteHost(u) {
  const m = /^[a-z][a-z0-9+.-]*:\/\/([^\/?#]*)/i.exec(u || "");
  if (!m) return false;
  const h = m[1].toLowerCase().replace(/^www\./, "");
  return h === "cherrymist.cafe" || h.endsWith(".cherrymist.cafe");
}

module.exports = {
  name: "cherrymist",
  apiVersion: 1,
  requiresBrowser: false,

  probe: isSiteHost,

  toc: (ctx, url) => {
    const key = seriesKey(ctx, url);
    const series = fetchJSON(ctx, API + "/series/" + encodeURIComponent(key));
    if (!series || !series.id) ctx.fail("site_layout_changed", "series " + key + " not found");

    return {
      novel: {
        title: clean(series.title),
        author: clean(authorOf(series)),
        description: series.short_synopsis ? series.short_synopsis : series.synopsis || "",
        coverUrl: series.cover_image_url || "",
        language: "",
        tags: []
      },
      chapters: chapterRefs(ctx, series.id)
    };
  },

  chapter: (ctx, url) => {
    const parts = slugs(ctx, url);
    if (parts[1] === "") ctx.fail("not_my_site", url + " is not a cherrymist chapter URL");

    const ref = chapterRef(ctx, parts[1]);
    const ch = fetchJSON(ctx, API + "/chapters/" + ref.id);
    if (!ch.content || String(ch.content).trim() === "") {
      ctx.fail("site_layout_changed", "chapter " + ch.slug + " returned no body");
    }

    const body = ch.cipher ? decodeBody(ch.content, ch.cipher.seed) : ch.content;
    const title = ch.title ? ch.title : ch.slug || "";
    return { title: clean(title), contentHtml: paragraphs(body) };
  }
};
