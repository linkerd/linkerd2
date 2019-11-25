import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import PropTypes from 'prop-types';
import React from 'react';
import Typography from '@material-ui/core/Typography';
import { withStyles } from '@material-ui/core/styles';
import { withTranslation } from 'react-i18next';

const styles = () => ({
  card: {
    textAlign: "center",
    paddingTop: "8px",
  },
});

const EmptyCard = ({ content, classes, t }) => {
  return (
    <Card className={classes.card}>
      <CardContent>
        <Typography>
          {t(content)}
        </Typography>
      </CardContent>
    </Card>
  );
};

EmptyCard.propTypes = {
  classes: PropTypes.shape({}).isRequired,
  content: PropTypes.string,
  t: PropTypes.func.isRequired,
};

EmptyCard.defaultProps = {
  content: "No data to display",
};

export default withTranslation()(withStyles(styles)(EmptyCard));
