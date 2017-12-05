import React from 'react';

export default class Metric extends React.Component {
  render() {
    return (
      <div className={`metric ${this.props.className || ""}`}>
        <div className="metric-title">{this.props.title}</div>
        <div className="metric-value">{this.props.value}</div>
      </div>
    );
  }
}