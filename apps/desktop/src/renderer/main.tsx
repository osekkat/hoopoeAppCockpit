import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { router } from "./routes.tsx";
import { ErrorBoundary } from "./error-ux/ErrorBoundary.tsx";
import { installRendererWindowErrorHandlers } from "./error-ux/windowErrorHandlers.ts";
import "./error-ux/error-ux.css";
import "./styles.css";

const rootElement = document.getElementById("root");

if (!rootElement) {
  throw new Error("Hoopoe renderer root element is missing");
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
});

installRendererWindowErrorHandlers();

// hp-sgy: wrap the entire renderer tree in an ErrorBoundary so a
// thrown render error in any descendant — TanStack Router route
// component, stage panel, store-derived selector, etc. — surfaces a
// recoverable fallback instead of unmounting the window.
createRoot(rootElement).render(
  <StrictMode>
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </ErrorBoundary>
  </StrictMode>,
);
