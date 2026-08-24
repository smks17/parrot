"use client";

import { useState } from "react";
import BootSequence from "@/components/BootSequence";
import Terminal from "@/components/Terminal";

export default function Home() {
  const [booted, setBooted] = useState(false);

  if (!booted) {
    return <BootSequence onDone={() => setBooted(true)} />;
  }
  return <Terminal />;
}
