import TerminalBackdrop from "./components/TerminalBackdrop";
import Navbar from "./components/Navbar";
import Hero from "./components/Hero";
import SourceReplicaStudio from "./components/SourceReplicaStudio";
import InstallDeck from "./components/InstallDeck";
import FinalCTA from "./components/FinalCTA";
import Footer from "./components/Footer";

export default function App() {
  return (
    <div className="site">
      <TerminalBackdrop />
      <Navbar />
      <main>
        <Hero />
        <SourceReplicaStudio />
        <InstallDeck />
        <FinalCTA />
      </main>
      <Footer />
    </div>
  );
}
