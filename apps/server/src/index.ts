import express from "express";
import cors from "cors";
import path from "path";
import { initializeDatabase } from "./db";
import publicRoutes from "./routes/public";
import dashboardRoutes from "./routes/dashboard";

const app: ReturnType<typeof express> = express();
const PORT = process.env.PORT || 4001;

// Middleware
app.use(cors());
app.use(express.json());

// Serve uploaded files
app.use("/uploads", express.static(path.join(__dirname, "..", "uploads")));

// Routes
app.use("/api", publicRoutes);
app.use("/api", dashboardRoutes);

// Health check
app.get("/api/health", (_req, res) => {
  res.json({ status: "ok" });
});

// Initialize database and start server
initializeDatabase();

app.listen(PORT, () => {
  console.log(`Server running on http://localhost:${PORT}`);
});

export default app;
