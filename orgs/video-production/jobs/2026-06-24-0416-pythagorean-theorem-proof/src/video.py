from manim import *
import numpy as np

class PythagoreanProof(Scene):
    def construct(self):
        scale = 0.7
        a_len = 3 * scale
        b_len = 4 * scale
        c_len = 5 * scale

        A = np.array([0.0, 0.0, 0.0])
        B = np.array([0.0, a_len, 0.0])
        C = np.array([b_len, 0.0, 0.0])

        # ----------------------------------------------------------------
        # 1. RIGHT TRIANGLE (0-5s)
        # ----------------------------------------------------------------
        triangle = Polygon(A, B, C, color=WHITE, stroke_width=5)
        sq_small = 0.15
        right_angle = Polygon(
            np.array([sq_small, 0, 0]),
            np.array([sq_small, sq_small, 0]),
            np.array([0, sq_small, 0]),
            color=WHITE, stroke_width=3, fill_opacity=0
        )

        self.play(Create(triangle), run_time=2.0)
        self.play(Create(right_angle), run_time=0.5)
        self.wait(1.0)

        label_a = Text("a", color=YELLOW, font_size=48).move_to(
            (A + B) / 2 + np.array([-0.5, 0, 0])
        )
        label_b = Text("b", color=YELLOW, font_size=48).move_to(
            (A + C) / 2 + np.array([0, -0.5, 0])
        )
        label_c = Text("c", color=YELLOW, font_size=48).move_to(
            (B + C) / 2 + np.array([0.4, 0.4, 0])
        )

        self.play(Write(label_a), Write(label_b), Write(label_c), run_time=1.5)
        self.wait(0.5)

        # ----------------------------------------------------------------
        # 2. DRAW SQUARES (5-14s)
        # ----------------------------------------------------------------
        sq_a_verts = [A, B, np.array([-a_len, a_len, 0]), np.array([-a_len, 0, 0])]
        sq_a = Polygon(*sq_a_verts, color=BLUE, stroke_width=4, fill_opacity=0.25, fill_color=BLUE)
        sq_a_label = Text("a²", color=BLUE, font_size=40).move_to(np.array([-a_len/2, a_len/2, 0]))

        sq_b_verts = [A, C, np.array([b_len, -b_len, 0]), np.array([0.0, -b_len, 0])]
        sq_b = Polygon(*sq_b_verts, color=GREEN, stroke_width=4, fill_opacity=0.25, fill_color=GREEN)
        sq_b_label = Text("b²", color=GREEN, font_size=40).move_to(np.array([b_len/2, -b_len/2, 0]))

        v = C - B
        w = np.array([a_len, b_len, 0])
        sq_c_verts = [B, C, C + w, B + w]
        sq_c = Polygon(*sq_c_verts, color=GOLD, stroke_width=4, fill_opacity=0.25, fill_color=GOLD)
        sq_c_center = (B + C + C + w + B + w) / 4
        sq_c_label = Text("c²", color=GOLD, font_size=40).move_to(sq_c_center)

        self.play(Create(sq_a), run_time=1.2)
        self.play(Write(sq_a_label), run_time=0.5)
        self.wait(0.5)

        self.play(Create(sq_b), run_time=1.2)
        self.play(Write(sq_b_label), run_time=0.5)
        self.wait(0.5)

        self.play(Create(sq_c), run_time=1.5)
        self.play(Write(sq_c_label), run_time=0.5)
        self.wait(1.0)

        # ----------------------------------------------------------------
        # 3. VISUAL REARRANGEMENT (14-22s)
        # ----------------------------------------------------------------
        self.play(
            sq_a.animate.set_stroke(YELLOW, width=6).set_fill(YELLOW, opacity=0.4),
            sq_b.animate.set_stroke(YELLOW, width=6).set_fill(YELLOW, opacity=0.4),
            run_time=1.0
        )

        target_a_center = sq_c_center + np.array([-c_len/4, c_len/4, 0])
        target_b_center = sq_c_center + np.array([c_len/4, -c_len/4, 0])

        self.play(
            sq_a.animate.move_to(target_a_center).scale(0.65).set_opacity(0.4),
            sq_b.animate.move_to(target_b_center).scale(0.75).set_opacity(0.4),
            run_time=2.0,
        )

        self.play(
            sq_c.animate.set_fill(YELLOW, opacity=0.5).set_stroke(YELLOW, width=6),
            run_time=1.0,
        )

        self.play(
            sq_c.animate.set_fill(GOLD, opacity=0.25).set_stroke(GOLD, width=4),
            sq_a.animate.set_fill(BLUE, opacity=0.25).set_stroke(BLUE, width=4).move_to(
                np.array([-a_len / 2, a_len / 2, 0])
            ).scale(1.0/0.65),
            sq_b.animate.set_fill(GREEN, opacity=0.25).set_stroke(GREEN, width=4).move_to(
                np.array([b_len / 2, -b_len / 2, 0])
            ).scale(1.0/0.75),
            run_time=1.5,
        )

        self.wait(0.5)

        # ----------------------------------------------------------------
        # 4. EQUATION APPEARS (22-28s)
        # ----------------------------------------------------------------
        eq = Text("a² + b² = c²", color=WHITE, font_size=52)
        eq.to_edge(DOWN, buff=0.8)

        underline = Line(
            eq.get_left() + np.array([-0.2, -0.15, 0]),
            eq.get_right() + np.array([0.2, -0.15, 0]),
            color=YELLOW, stroke_width=3,
        )

        self.play(Write(eq), run_time=1.5)
        self.play(Create(underline), run_time=0.5)
        self.wait(2.0)

        self.play(
            eq.animate.scale(1.05).set_color(YELLOW), run_time=0.5,
        )
        self.play(
            eq.animate.scale(1.0 / 1.05).set_color(WHITE), run_time=0.5,
        )
        self.wait(3.0)

        # ----------------------------------------------------------------
        # 5. FINAL FADE OUT
        # ----------------------------------------------------------------
        all_mobs = VGroup(
            triangle, right_angle,
            label_a, label_b, label_c,
            sq_a, sq_a_label,
            sq_b, sq_b_label,
            sq_c, sq_c_label,
            eq, underline,
        )
        self.play(FadeOut(all_mobs), run_time=2.5)
        self.wait(2.0)
