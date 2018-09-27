import React from 'react';

const stroke = "#000000";
const strokeOpacity = "0.2";

const svgArrow = (x1, y1, width, height, direction) => {
  let segmentWidth = width / 2 - 10;

  let x2 = x1 + segmentWidth + 10;
  let y2 = y1 + 10;

  let x3 = x2 + 10;
  let y3 = y2 + height + 10;

  let horizLine1 = `M ${x1},${y1} L ${x1+segmentWidth},${y1}`;
  let curve1 = `C ${x1+segmentWidth+7},${y1} ${x2},${y1+3} ${x2},${y2}`;
  let verticalLine = `L ${x2}, ${y2+height}`;
  let curve2 = `C ${x2},${y2+height+6} ${x2+2},${y3} ${x3},${y3}`;
  let horizLine2 = `L ${x3 + segmentWidth},${y3}`;
  let arrow = `${horizLine1} ${curve1} ${verticalLine} ${curve2} ${horizLine2}`;

  let arrowEndX = width;
  let arrowEndY = direction === "up" ? y1 : y3;
  let arrowHead = `${arrowEndX - 4} ${arrowEndY - 4} ${arrowEndX} ${arrowEndY} ${arrowEndX - 4} ${arrowEndY + 4}`;

  let circle = {
    cx: x1,
    cy: direction === "up" ? y3 : y1
  };

  return {
    arrow,
    arrowHead,
    circle
  };
};

const up = (width, svgHeight, arrowHeight, isOutbound) => {
  let height = (svgHeight / 2) - arrowHeight;

  let x1 = 0;
  let y1;

  if (isOutbound) {
    y1 = arrowHeight - 20; // I don't know where we intoduced that 20 but we need to match the other arrow
  } else {
    y1 = svgHeight / 2 ;
  }

  let svgPaths = svgArrow(x1, y1, width, height, "up");

  return (
    <g key={`up-arrow-${height}`} id="downstream-up" fill="none" stroke="none" strokeWidth="1">
      <path
        d={svgPaths.arrow}
        stroke={stroke}
        strokeOpacity={strokeOpacity}
        transform="translate(31, 54) scale(-1, 1) translate(-44, -54) " />
      <circle cx={svgPaths.circle.cx} cy={svgPaths.circle.cy} fill="#CCCCCC" r="4" />
      <polyline points={svgPaths.arrowHead} stroke="#CCCCCC" strokeLinecap="round" />
    </g>

  );
};

const flat = (width, height) => {
  let arrowY = height / 2;
  let arrowEndX = width;
  let polylinePoints = `${arrowEndX - 4} ${arrowY - 4} ${arrowEndX} ${arrowY} ${arrowEndX - 4} ${arrowY + 4}`;

  return (
    <g key="flat-arrow" id="downstream-flat" fill="none" stroke="none" strokeWidth="1">
      <path
        d={`M0,${arrowY} L${arrowEndX},${arrowY}`}
        stroke={stroke}
        strokeOpacity={strokeOpacity} />
      <circle cx="0" cy={arrowY} fill="#CCCCCC" r="4" />
      <polyline points={polylinePoints} stroke="#CCCCCC" strokeLinecap="round" />
    </g>
  );
};

const down = (width, svgHeight, arrowHeight, isOutbound) => {
  // down outbound arrows start at the middle of the svg's height, and
  // have end of block n at (1/2 block height) + (block height * n-1)
  let height = (svgHeight / 2) - arrowHeight;

  let x1 = 0;
  let y1;

  // inbound arrows start at the offset of the card, and end in the center of the middle card
  // outbound arrows start in the center of the middle card, and end at the card's height
  if (isOutbound) {
    y1 = svgHeight / 2;
  } else {
    y1 = arrowHeight - 20; // I don't know where we intoduced that 20 but we need to match the other arrow
  }

  let svg = svgArrow(x1, y1, width, height, "down");

  return (
    <g key={`down-arrow-${height}`} id="downstream-down" fill="none" stroke="none" strokeWidth="1">
      <path
        d={svg.arrow}
        stroke={stroke}
        strokeOpacity={strokeOpacity} />
      <circle cx={svg.circle.cx} cy={svg.circle.cy} fill="#CCCCCC" r="4" />
      <polyline points={svg.arrowHead} stroke="#CCCCCC" strokeLinecap="round" />
    </g>

  );
};

const OctopusArms = {
  up,
  flat,
  down
};

export default OctopusArms;
