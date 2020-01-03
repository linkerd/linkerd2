import React from 'react';
import grey from '@material-ui/core/colors/grey';

const strokeOpacity = '0.7';
const arrowColor = grey[500];

const controlPoint = 10; // width and height of the control points for the bezier curves
export const inboundAlignment = controlPoint * 2;

const generateSvgComponents = (y1, width, height) => {
  const segmentWidth = width / 2 - controlPoint; // width of each horizontal arrow segment

  const x1 = 0;

  const x2 = x1 + segmentWidth;
  const x3 = x2 + controlPoint;

  const y2 = y1 - controlPoint;
  const y3 = y2 - height;
  const y4 = y3 - controlPoint;

  const x4 = x3 + controlPoint;
  const x5 = x4 + segmentWidth;

  const start = `M ${x1},${y1}`;
  const horizLine1 = `L ${x2},${y1}`;
  const curve1 = `C ${x3},${y1} ${x3},${y1}`;
  const curve1End = `${x3},${y2}`;
  const verticalLineEnd = `L ${x3},${y3}`;
  const curve2 = `C ${x3},${y4} ${x3},${y4}`;
  const curve2End = `${x4},${y4}`;
  const horizLine2 = `L ${x5},${y4}`;

  const arrowPath = [start, horizLine1, curve1, curve1End, verticalLineEnd, curve2, curve2End, horizLine2].join(' ');

  const arrowEndX = width;
  const arrowEndY = y4;
  const arrowHead = `${arrowEndX - 4} ${arrowEndY - 4} ${arrowEndX} ${arrowEndY} ${arrowEndX - 4} ${arrowEndY + 4}`;

  const circle = { cx: x1, cy: y1 };

  return {
    arrowPath,
    circle,
    arrowHead,
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

const up = (width, svgHeight, arrowHeight, isOutbound) => {
  const height = arrowHeight;

  // up arrows start and the center of the middle node for outbound arms,
  // and at the noce position for inbound arms
  const y1 = isOutbound ? svgHeight / 2 : arrowHeight;
  const arm = generateSvgComponents(y1, width, height);

  const translate = isOutbound ? null : `translate(0, ${svgHeight / 2 + inboundAlignment})`;

  return arrowG(`up-arrow-${height}`, arm, translate);
};

const flat = (width, height) => {
  const arrowY = height / 2;
  const arrowEndX = width;
  const polylinePoints = `${arrowEndX - 4} ${arrowY - 4} ${arrowEndX} ${arrowY} ${arrowEndX - 4} ${arrowY + 4}`;

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

const down = (width, svgHeight, arrowHeight, isOutbound, elementHeight) => {
  // down outbound arrows start at the middle of the svg's height, and
  // have end of block n at (1/2 block height) + (block height * n-1)
  const height = (svgHeight / 2) - arrowHeight;

  // inbound arrows start at the offset of the card, and end in the center of the middle card
  // outbound arrows start in the center of the middle card, and end at the card's height
  const y1 = isOutbound ? svgHeight / 2 : elementHeight / 2;

  const arm = generateSvgComponents(y1, width, height);

  const translate = `translate(0, ${isOutbound ? svgHeight : svgHeight / 2 - height + elementHeight / 2 - inboundAlignment})`;
  const reflect = 'scale(1, -1)';
  const transform = `${translate} ${reflect}`;

  return arrowG(`down-arrow-${height}`, arm, transform);
};

export const OctopusArms = {
  up,
  flat,
  down,
};
