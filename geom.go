package main

// Plain geometry shared by the drawing code on every platform.

type POINT struct{ X, Y int32 }
type SIZE struct{ CX, CY int32 }
type RECT struct{ Left, Top, Right, Bottom int32 }
type RectF struct{ X, Y, W, H float32 }
type PointF struct{ X, Y float32 }
