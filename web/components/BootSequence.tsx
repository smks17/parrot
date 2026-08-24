"use client";

import { useEffect, useRef, useState } from "react";
import { bootLines } from "@/lib/boot-sequence";
import styles from "./BootSequence.module.css";

const MS_PER_CHAR = 18;

interface Props {
  onDone: () => void;
}


export default function BootSequence({ onDone }: Props) {
  const [shown, setShown] = useState("");
  const doneRef = useRef(false);

  const finish = () => {
    if (doneRef.current) return;
    doneRef.current = true;
    onDone();
  };

  useEffect(() => {
    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const fullText = bootLines.join("\n");

    if (reducedMotion) {
      setTimeout(() => setShown(fullText), 0);
      const timer = setTimeout(finish, 400);
      return () => clearTimeout(timer);
    }

    let i = 0;
    const interval = setInterval(() => {
      i++;
      setShown(fullText.slice(0, i));
      if (i >= fullText.length) {
        clearInterval(interval);
        setTimeout(finish, 300);
      }
    }, MS_PER_CHAR);

    const skip = () => {
      clearInterval(interval);
      setShown(fullText);
      finish();
    };
    window.addEventListener("keydown", skip);
    window.addEventListener("click", skip);

    return () => {
      clearInterval(interval);
      window.removeEventListener("keydown", skip);
      window.removeEventListener("click", skip);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- runs once by design
  }, []);

  return (
    <div className={styles.boot}>
      <pre className={styles.text}>{shown}</pre>
    </div>
  );
}
