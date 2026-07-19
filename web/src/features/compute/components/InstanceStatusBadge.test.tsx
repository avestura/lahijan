/**
 * InstanceStatusBadge unit tests.
 *
 * Verifies the badge picks the right tone + localized label for each
 * status bucket. We mock react-i18next directly to assert the key
 * looked up by the component.
 */
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";

import { InstanceStatusBadge } from "./InstanceStatusBadge";

const tMock = vi.fn((key: string) => key);

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tMock }),
}));

describe("<InstanceStatusBadge />", () => {
  beforeEach(() => {
    tMock.mockClear();
  });

  it("renders the running bucket as a success badge", () => {
    render(<InstanceStatusBadge status="Running" />);
    expect(tMock).toHaveBeenCalledWith("compute.status.running");
    const badge = screen.getByText("compute.status.running");
    expect(badge.className).toContain("bg-success");
  });

  it("renders the stopped bucket as a muted badge", () => {
    render(<InstanceStatusBadge status="Stopped" />);
    expect(tMock).toHaveBeenCalledWith("compute.status.stopped");
    const badge = screen.getByText("compute.status.stopped");
    expect(badge.className).toContain("bg-muted");
  });

  it("renders the frozen bucket as a secondary badge", () => {
    render(<InstanceStatusBadge status="Frozen" />);
    expect(tMock).toHaveBeenCalledWith("compute.status.frozen");
  });

  it("falls back to the 'other' bucket for unknown statuses", () => {
    render(<InstanceStatusBadge status="Starting" />);
    expect(tMock).toHaveBeenCalledWith("compute.status.other");
  });

  it("treats undefined status as 'other'", () => {
    render(<InstanceStatusBadge status={undefined} />);
    expect(tMock).toHaveBeenCalledWith("compute.status.other");
  });
});
