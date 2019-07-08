import React from 'react';
import grey from '@material-ui/core/colors/grey';

const strokeOpacity = "0.7";
const arrowColor = grey[500];

export const baseHeight = 242; // the height of the neighbor node box plus padding
const halfBoxHeight = baseHeight / 2;
const controlPoint = 10; // width and height of the control points for the bezier curves
const inboundAlignment = controlPoint * 2;

const generateSvgComponents = (y1, width, height) => {
  let segmentWidth = width / 2 - controlPoint; // width of each horizontal arrow segment

  let x1 = 0;

  let x2 = x1 + segmentWidth;
  let x3 = x2 + controlPoint;

  let y2 = y1 - controlPoint;
  let y3 = y2 - height;
  let y4 = y3 - controlPoint;

  let x4 = x3 + controlPoint;
  let x5 = x4 + segmentWidth;

  let start = `M ${x1},${y1}`;
  let horizLine1 = `L ${x2},${y1}`;
  let curve1 = `C ${x3},${y1} ${x3},${y1}`;
  let curve1End = `${x3},${y2}`;
  let verticalLineEnd = `L ${x3},${y3}`;
  let curve2 = `C ${x3},${y4} ${x3},${y4}`;
  let curve2End = `${x4},${y4}`;
  let horizLine2 = `L ${x5},${y4}`;

  let arrowPath = [start, horizLine1, curve1, curve1End, verticalLineEnd, curve2, curve2End, horizLine2].join(" ");

  let arrowEndX = width;
  let arrowEndY = y4;
  let arrowHead = `${arrowEndX - 4} ${arrowEndY - 4} ${arrowEndX} ${arrowEndY} ${arrowEndX - 4} ${arrowEndY + 4}`;

  let circle = { cx: x1, cy: y1 };

  return {
    arrowPath,
    circle,
    arrowHead
  };
};

const arrowG = (id, arm, transform) => {
  return (
    <g key={id} id={id} fill="none" strokeWidth="1">
      <path
        d={arm.arrowPath}
        stroke={arrowColor}
        transform={transform}
        strokeOpacity={strokeOpacity} />
      <circle
        cx={arm.circle.cx}
        cy={arm.circle.cy}
        transform={transform}
        fill={arrowColor}
        r="4" />
      <polyline
        points={arm.arrowHead}
        stroke={arrowColor}
        strokeLinecap="round"
        transform={transform} />
    </g>
  );
};

const up = (width, svgHeight, arrowHeight, isOutbound, isEven) => {
  let height = arrowHeight + (isEven ? 0 : halfBoxHeight) - controlPoint * 2;

  // up arrows start and the center of the middle node for outbound arms,
  // and at the noce position for inbound arms
  let y1 = isOutbound ? svgHeight / 2 : arrowHeight;
  let arm = generateSvgComponents(y1, width, height);

  let translate = isOutbound ? null : `translate(0, ${svgHeight / 2 + (isEven ? 0 : halfBoxHeight) + inboundAlignment - controlPoint * 2})`;

  return arrowG(`up-arrow-${height}`, arm, translate);
};

const flat = (width, height) => {
  let arrowY = height / 2;
  let arrowEndX = width;
  let polylinePoints = `${arrowEndX - 4} ${arrowY - 4} ${arrowEndX} ${arrowY} ${arrowEndX - 4} ${arrowY + 4}`;

  return (
    <g key="flat-arrow" id="downstream-flat" fill="none" stroke="none" strokeWidth="1">
      <path
        d={`M0,${arrowY} L${arrowEndX},${arrowY}`}
        stroke={arrowColor}
        strokeOpacity={strokeOpacity} />
      <circle cx="0" cy={arrowY} fill={arrowColor} r="4" />
      <polyline points={polylinePoints} stroke={arrowColor} strokeLinecap="round" />
    </g>
  );
};

const down = (width, svgHeight, arrowHeight, isOutbound) => {
  // down outbound arrows start at the middle of the svg's height, and
  // have end of block n at (1/2 block height) + (block height * n-1)
  let height = svgHeight / 2 - arrowHeight - controlPoint * 2;

  // inbound arrows start at the offset of the card, and end in the center of the middle card
  // outbound arrows start in the center of the middle card, and end at the card's height
  let y1 = isOutbound ? svgHeight / 2 : halfBoxHeight;

  let arm = generateSvgComponents(y1, width, height);

  let translate = `translate(0, ${isOutbound ? svgHeight : svgHeight / 2 - height + halfBoxHeight - inboundAlignment})`;
  let reflect = "scale(1, -1)";
  let transform = `${translate} ${reflect}`;

  return arrowG(`down-arrow-${height}`, arm, transform);
};

export const OctopusArms = {
  up,
  flat,
  down
};
