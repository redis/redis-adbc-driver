// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

// zoneAbbrev is a time zone abbreviation: a fixed offset, or a zone in
// which it means the offset it has there (abbrevZone).
type zoneAbbrev struct {
	off  int    // seconds east of UTC
	zone string // for an abbreviation tied to a zone (MSK: Europe/Moscow)
}

// zoneAbbrevs are the time zone abbreviations of Postgres's default set
// (timezone_abbreviations = 'Default'), by lower-case name: every entry of
// Postgres 16's share/timezonesets/Default (src/timezone/tznames/Default,
// under the PostgreSQL License), with its offset in seconds east of UTC or
// its zone. Postgres looks them up before zone names, so 'CET' is always
// UTC+1 (the IANA zone CET observes summer time). IST is Israel Standard
// Time, UTC+2.
var zoneAbbrevs = map[string]zoneAbbrev{
	// Africa
	"eat": {off: 10800}, "sast": {off: 7200}, "wat": {off: 3600},
	// America
	"act": {off: -18000}, "akdt": {off: -28800}, "akst": {off: -32400},
	"art":  {zone: "America/Argentina/Buenos_Aires"},
	"arst": {zone: "America/Argentina/Buenos_Aires"}, "bot": {off: -14400},
	"bra": {off: -10800}, "brst": {off: -7200}, "brt": {off: -10800},
	"cot": {off: -18000}, "cdt": {off: -18000}, "clst": {off: -10800},
	"clt": {zone: "America/Santiago"}, "cst": {off: -21600},
	"edt": {off: -14400}, "egst": {off: 0}, "egt": {off: -3600},
	"est": {off: -18000}, "fnt": {off: -7200}, "fnst": {off: -3600},
	"gft": {off: -10800}, "gyt": {zone: "America/Guyana"},
	"mdt": {off: -21600}, "mst": {off: -25200}, "ndt": {off: -9000},
	"nft": {off: -12600}, "nst": {off: -12600}, "pet": {off: -18000},
	"pdt": {off: -25200}, "pmdt": {off: -7200}, "pmst": {off: -10800},
	"pst": {off: -28800}, "pyst": {off: -10800},
	"pyt": {zone: "America/Asuncion"}, "uyst": {off: -7200},
	"uyt": {off: -10800}, "vet": {zone: "America/Caracas"},
	"wgst": {off: -7200}, "wgt": {off: -10800},
	// Antarctica
	"davt": {zone: "Antarctica/Davis"}, "ddut": {off: 36000},
	"mawt": {zone: "Antarctica/Mawson"},
	// Asia
	"aft": {off: 16200}, "almt": {off: 21600}, "almst": {off: 25200},
	"amst": {zone: "Asia/Yerevan"}, "amt": {off: -14400},
	"anast": {zone: "Asia/Anadyr"}, "anat": {zone: "Asia/Anadyr"},
	"azst": {zone: "Asia/Baku"}, "azt": {zone: "Asia/Baku"},
	"bdt": {off: 21600}, "bnt": {off: 28800}, "bort": {off: 28800},
	"btt": {off: 21600}, "cct": {off: 28800}, "gest": {zone: "Asia/Tbilisi"},
	"get": {zone: "Asia/Tbilisi"}, "hkt": {off: 28800}, "ict": {off: 25200},
	"idt": {off: 10800}, "irkst": {zone: "Asia/Irkutsk"},
	"irkt": {zone: "Asia/Irkutsk"}, "irt": {off: 12600}, "ist": {off: 7200},
	"jayt": {off: 32400}, "jst": {off: 32400}, "kdt": {off: 36000},
	"kgst": {off: 21600}, "kgt": {zone: "Asia/Bishkek"},
	"krast": {zone: "Asia/Krasnoyarsk"}, "krat": {zone: "Asia/Krasnoyarsk"},
	"kst": {off: 32400}, "lkt": {zone: "Asia/Colombo"},
	"magst": {zone: "Asia/Magadan"}, "magt": {zone: "Asia/Magadan"},
	"mmt": {off: 23400}, "myt": {off: 28800},
	"novst": {zone: "Asia/Novosibirsk"}, "novt": {zone: "Asia/Novosibirsk"},
	"npt": {off: 20700}, "omsst": {zone: "Asia/Omsk"},
	"omst": {zone: "Asia/Omsk"}, "petst": {zone: "Asia/Kamchatka"},
	"pett": {zone: "Asia/Kamchatka"}, "pht": {off: 28800},
	"pkt": {off: 18000}, "pkst": {off: 21600},
	"sgt": {zone: "Asia/Singapore"}, "tjt": {off: 18000},
	"tmt": {zone: "Asia/Ashgabat"}, "ulast": {off: 32400},
	"ulat": {zone: "Asia/Ulaanbaatar"}, "uzst": {off: 21600},
	"uzt": {off: 18000}, "vlast": {zone: "Asia/Vladivostok"},
	"vlat": {zone: "Asia/Vladivostok"}, "xjt": {off: 21600},
	"yakst": {zone: "Asia/Yakutsk"}, "yakt": {zone: "Asia/Yakutsk"},
	"yekst": {off: 21600}, "yekt": {zone: "Asia/Yekaterinburg"},
	// Atlantic
	"adt": {off: -10800}, "ast": {off: -14400}, "azost": {off: 0},
	"azot": {off: -3600}, "fkst": {zone: "Atlantic/Stanley"},
	"fkt": {zone: "Atlantic/Stanley"},
	// Australia
	"acsst": {off: 37800}, "acdt": {off: 37800}, "acst": {off: 34200},
	"acwst": {off: 31500}, "aesst": {off: 39600}, "aedt": {off: 39600},
	"aest": {off: 36000}, "awsst": {off: 32400}, "awst": {off: 28800},
	"cadt": {off: 37800}, "cast": {off: 34200},
	"lhdt": {zone: "Australia/Lord_Howe"}, "lhst": {off: 37800},
	"ligt": {off: 36000}, "nzt": {off: 43200}, "sadt": {off: 37800},
	"wadt": {off: 28800}, "wast": {off: 25200}, "wdt": {off: 32400},
	// Etc
	"gmt": {off: 0}, "uct": {off: 0}, "ut": {off: 0}, "utc": {off: 0},
	"z": {off: 0}, "zulu": {off: 0},
	// Europe
	"bst": {off: 3600}, "bdst": {off: 7200}, "cest": {off: 7200},
	"cet": {off: 3600}, "cetdst": {off: 7200}, "eest": {off: 10800},
	"eet": {off: 7200}, "eetdst": {off: 10800}, "fet": {off: 10800},
	"mest": {off: 7200}, "mesz": {off: 7200}, "met": {off: 3600},
	"metdst": {off: 7200}, "mez": {off: 3600}, "msd": {off: 14400},
	"msk": {zone: "Europe/Moscow"}, "volt": {zone: "Europe/Volgograd"},
	"wet": {off: 0}, "wetdst": {off: 3600},
	// Indian
	"cxt": {off: 25200}, "iot": {zone: "Indian/Chagos"}, "mut": {off: 14400},
	"must": {off: 18000}, "mvt": {off: 18000}, "ret": {off: 14400},
	"sct": {off: 14400}, "tft": {off: 18000},
	// Pacific
	"chadt": {off: 49500}, "chast": {off: 45900}, "chut": {off: 36000},
	"ckt": {zone: "Pacific/Rarotonga"}, "easst": {zone: "Pacific/Easter"},
	"east": {zone: "Pacific/Easter"}, "fjst": {off: 46800},
	"fjt": {off: 43200}, "galt": {off: -21600}, "gamt": {off: -32400},
	"gilt": {off: 43200}, "hst": {off: -36000},
	"kost": {zone: "Pacific/Kosrae"}, "lint": {zone: "Pacific/Kiritimati"},
	"mart": {off: -34200}, "mht": {off: 43200}, "mpt": {off: 36000},
	"nut": {zone: "Pacific/Niue"}, "nzdt": {off: 46800}, "nzst": {off: 43200},
	"pgt": {off: 36000}, "pont": {off: 39600}, "pwt": {off: 32400},
	"taht": {off: -36000}, "tkt": {zone: "Pacific/Fakaofo"},
	"tot": {off: 46800}, "trut": {off: 36000}, "tvt": {off: 43200},
	"vut": {off: 39600}, "wakt": {off: 43200}, "wft": {off: 43200},
	"yapt": {off: 36000},
}
