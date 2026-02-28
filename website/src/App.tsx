import Navbar from "./components/Navbar";
import Hero from "./components/Hero";
import Footer from "./components/Footer";

export default function App() {
  return (
    <div className="site">
      <Navbar />
      <main>
        <Hero />
      </main>
      <Footer />
    </div>
  );
}
