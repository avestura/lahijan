import { describe, expect, it } from "vitest";

import { searchDocs } from "@/docs/search";

const INDEX = [
  {
    slug: "dns/zones",
    title: "Zones",
    description: "Create DNS zones.",
    headings: ["Create a zone"],
    text: "zone soa ns",
  },
  {
    slug: "dns/dnssec",
    title: "DNSSEC",
    description: "Sign zones.",
    headings: ["DS records"],
    text: "sign the zone",
  },
  {
    slug: "storage/buckets",
    title: "Buckets",
    description: "Store objects.",
    headings: ["Upload"],
    text: "s3 objects",
  },
];

describe("searchDocs", () => {
  it("returns nothing for an empty query", () => {
    expect(searchDocs(INDEX, "  ")).toEqual([]);
  });

  it("ranks title matches above body matches", () => {
    const r = searchDocs(INDEX, "zone");
    expect(r[0]?.slug).toBe("dns/zones");
    expect(r.map((x) => x.slug)).toContain("dns/dnssec");
  });

  it("reports the matching heading as context", () => {
    expect(searchDocs(INDEX, "ds records")[0]).toMatchObject({
      slug: "dns/dnssec",
      context: "DS records",
    });
  });

  it("requires every term to appear on the page", () => {
    expect(searchDocs(INDEX, "zone upload")).toEqual([]);
  });
});
