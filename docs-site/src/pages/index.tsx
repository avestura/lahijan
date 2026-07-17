import React from "react";
import { Redirect } from "@docusaurus/router";

// Index page → the docs README.
// The marketing website (../website/) is the actual landing page; this is
// just the docs site root.

export default function Home(): JSX.Element {
  return <Redirect to="/README" />;
}
