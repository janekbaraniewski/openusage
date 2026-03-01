import { useMemo, useState } from "react";
import AnimatedSection from "./ui/AnimatedSection";
import CompositeDashboard, { moduleSpecs, type ModuleId, ScaledModule } from "./CompositeDashboard";
import OpenCodeExactReplica from "./OpenCodeExactReplica";

export default function SourceReplicaStudio() {
  const [active, setActive] = useState<ModuleId>("claude");
  const selected = useMemo(() => moduleSpecs.find((s) => s.id === active) ?? moduleSpecs[0], [active]);
  const nativeScale = 1.92;
  const nativeBaseWidth = Math.round(selected.width / nativeScale);
  const nativeBodyWidth = nativeBaseWidth;

  return (
    <>
      <section className="section section-modules" id="modules">
        <div className="container">
          <AnimatedSection className="section-head">
            <p className="section-label">Dashboard Replica</p>
            <h2>Exact module set reconstructed in code.</h2>
            <p>
              The full board and each provider module are rendered via components. No screenshot images
              are used in these surfaces.
            </p>
          </AnimatedSection>

          <div className="board-shell">
            <div className="board-head">
              <span>compact 6-module layout</span>
              <span>3 x 2 grid</span>
            </div>
            <div className="board-body">
              <CompositeDashboard scale={0.235} />
            </div>
          </div>

          <div className="focus-shell">
            <div className="focus-rail" role="tablist" aria-label="Provider module selector">
              {moduleSpecs.map((spec) => (
                <button
                  key={spec.id}
                  type="button"
                  role="tab"
                  data-module-id={spec.id}
                  aria-selected={spec.id === active}
                  className={spec.id === active ? "is-active" : ""}
                  onClick={() => setActive(spec.id)}
                >
                  <span>{spec.title}</span>
                  <small>
                    {spec.width}x{spec.height}
                  </small>
                </button>
              ))}
            </div>

            <div className="focus-head">
              <span>{selected.title}</span>
              <span>native module size</span>
            </div>

            <div className="focus-viewport">
              <div
                className="native-shell"
                data-testid="native-module-shell"
                style={{ width: selected.width, height: selected.height }}
              >
                <div
                  className="native-shell-inner"
                  style={{
                    width: nativeBaseWidth,
                    transform: `scale(${nativeScale})`,
                    transformOrigin: "top left",
                  }}
                >
                  {selected.render(nativeBodyWidth)}
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      <section className="section section-side-by-side" id="side-by-side">
        <div className="container">
          <AnimatedSection className="section-head">
            <p className="section-label">Side-by-Side Runtime</p>
            <h2>OpenUsage next to OpenCode in one terminal operating view.</h2>
            <p>
              Reconstructed with component-rendered terminal panes and matched density/proportions.
            </p>
          </AnimatedSection>

          <div className="runtime-shell">
            <div className="runtime-head">
              <span>paired runtime view</span>
              <span>openusage + opencode</span>
            </div>

            <div className="runtime-body">
              <div className="runtime-col">
                <ScaledModule spec={selected} scale={0.39} />
              </div>
              <div className="runtime-col">
                <OpenCodeExactReplica />
              </div>
            </div>
          </div>
        </div>
      </section>
    </>
  );
}
