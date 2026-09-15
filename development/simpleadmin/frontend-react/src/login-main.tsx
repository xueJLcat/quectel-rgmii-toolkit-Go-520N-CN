import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import LoginApp from "@/app/LoginApp";
import "@/lib/i18n";
import "@/styles/globals.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <LoginApp />
  </StrictMode>,
);
